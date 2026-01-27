// Copyright 2025 The Envoy XDS Controller Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sds

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	xdserr "github.com/tentens-tech/xds-controller/pkg/xds/err"
)

// TLSSecretReconciler reconciles a TLSSecret object
type TLSSecretReconciler struct {
	client.Client
	Scheme                 *runtime.Scheme
	Config                 *xds.Config
	reconciling            atomic.Int32
	lastReconcileTime      atomic.Int64
	initialReconcileLogged atomic.Bool
}

//+kubebuilder:rbac:groups=envoyxds.io,resources=tlssecrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=envoyxds.io,resources=tlssecrets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=envoyxds.io,resources=tlssecrets/finalizers,verbs=update
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TLSSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	// Check for shutdown signal
	if ctx.Err() != nil {
		return ctrl.Result{}, nil
	}

	// Mark that we have domain configs to reconcile (handles dynamically added resources)
	r.Config.ReconciliationStatus.SetHasDomainConfigs(true)
	r.reconciling.Add(1)
	r.lastReconcileTime.Store(time.Now().UnixNano())

	// Create a child context with timeout
	// it will wait 130 seconds until the let's encrypt will be able to generate the certificate,
	// letsencrypt has 120 seconds timeout, so if something goes wrong, we will have another 10 seconds to release the lock
	// this will make sure that all certs are generated before snapshot is generated, but if any error it will release the lock and allow generate snapshot with failed sds
	reconcileCtx, cancel := context.WithTimeout(ctx, 130*time.Second)
	defer cancel()

	go func() {
		defer func() {
			time.Sleep(time.Second)
			count := r.reconciling.Add(-1)
			if count == 0 {
				r.Config.ReconciliationStatus.SetDomainConfigsReconciled(true)
				// Log only once when initial reconciliation completes
				if !r.initialReconcileLogged.Swap(true) {
					ctrl.Log.WithName("SDS").Info("SDS reconciliation complete")
				}
			}
		}()

		<-reconcileCtx.Done()
		if reconcileCtx.Err() == context.DeadlineExceeded {
			log.Info("Reconciliation timed out")
		}
	}()

	var tlsSecret envoyxdsv1alpha1.TLSSecret
	tlsSecretFound := true
	if err := r.Get(ctx, req.NamespacedName, &tlsSecret); err != nil { // nolint
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to get TLSSecret")
			return ctrl.Result{}, err
		}
		tlsSecretFound = false
	}

	// Set default nodes and clusters if not present
	if tlsSecret.Annotations == nil {
		tlsSecret.Annotations = make(map[string]string)
	}
	if tlsSecret.Annotations["nodes"] == "" && tlsSecretFound {
		tlsSecret.Annotations["nodes"] = r.Config.NodeID
	}
	if tlsSecret.Annotations["clusters"] == "" && tlsSecretFound {
		tlsSecret.Annotations["clusters"] = r.Config.Cluster
	}
	nodeid := util.GetNodeID(tlsSecret.Annotations)

	if !tlsSecretFound {
		r.Config.LockConfig()
		for node := range r.Config.SecretConfigs {
			for i, c := range r.Config.SecretConfigs[node] {
				if c.Name == req.Name {
					log.V(0).Info("Removing secret")
					r.Config.SecretConfigs[node] = append(r.Config.SecretConfigs[node][:i], r.Config.SecretConfigs[node][i+1:]...)
					r.Config.IncrementConfigCounter()
				}
			}
		}
		r.Config.UnlockConfig()
		return ctrl.Result{}, nil
	}

	tlsSecret.Spec.SecretName = req.Name
	sds, requeueAfter, expiration, err := GetSecret(ctx, &tlsSecret.Spec.DomainConfig, r.Config)
	days := int(requeueAfter.Hours()) / 24
	hours := int(requeueAfter.Hours()) % 24
	minutes := int(requeueAfter.Minutes()) % 60
	nextRenewal := fmt.Sprintf("%dd %dh %dm", days, hours, minutes)

	// Update only the annotation if it changed; avoid needless updates
	if !expiration.IsZero() {
		if tlsSecret.Annotations == nil {
			tlsSecret.Annotations = make(map[string]string)
		}
		newExp := expiration.Format(time.RFC3339)
		if tlsSecret.Annotations["cert-expiration"] != newExp {
			old := tlsSecret.DeepCopy()
			tlsSecret.Annotations["cert-expiration"] = newExp
			if patchErr := r.Patch(ctx, &tlsSecret, client.MergeFrom(old)); patchErr != nil {
				log.Error(patchErr, "failed to patch TLSSecret cert-expiration annotation")
			} else {
				log.V(2).Info("updated cert-expiration annotation", "value", newExp)
			}
		}
	}

	if requeueAfter <= 0 {
		requeueAfter = 30 * time.Second
	}

	// If we're not the leader and there's no certificate, just requeue
	if !r.Config.IsLeaderInstance() && (errors.Is(err, xdserr.ErrCertNotFound) || errors.Is(err, xdserr.ErrCertNil)) {
		log.V(2).Info("waiting for leader to generate certificate")
		return ctrl.Result{
			RequeueAfter: requeueAfter,
		}, nil
	}

	if err != nil {
		log.V(0).Error(err, "Error encountered while creating the secret. Please verify your SDS configuration. The secret will not be added.")
		xds.RecordConfigError(tlsSecret.Spec.SecretName, "SDS", "Error encountered while creating the secret. Please verify your SDS configuration. The secret will not be added.\n Next try: "+nextRenewal+"\n"+err.Error())
		// Update status with error
		if statusErr := r.updateTLSSecretStatus(ctx, &tlsSecret, false, nil, nextRenewal, nil, err.Error()); statusErr != nil {
			log.Error(statusErr, "unable to update TLSSecret status")
		}
		return ctrl.Result{
			RequeueAfter: requeueAfter,
		}, nil
	}

	// Extract certificate info for status
	certInfo := extractCertificateInfo(sds)

	sds.Name = tlsSecret.Name

	// Use granular locking for SecretConfigs access to allow parallel reconciliation
	r.Config.LockConfig()
	var found bool
	var foundNode string
	var needsReplace bool
	var unchanged bool

	for node := range r.Config.SecretConfigs {
		for k, v := range r.Config.SecretConfigs[node] {
			if v.Name == tlsSecret.Name {
				found = true
				foundNode = node
				if nodeid != node {
					needsReplace = true
					log.V(0).Info("Replacing secret", "next_renewal", nextRenewal)
					r.Config.SecretConfigs[node] = append(r.Config.SecretConfigs[node][:k], r.Config.SecretConfigs[node][k+1:]...)
					if r.Config.SecretConfigs[nodeid] == nil {
						r.Config.SecretConfigs[nodeid] = []*auth.Secret{}
					}
					r.Config.SecretConfigs[nodeid] = append(r.Config.SecretConfigs[nodeid], sds)
					r.Config.IncrementConfigCounter()
				} else {
					// Avoid reapplying config if secret content has not changed
					if secretsEqual(r.Config.SecretConfigs[node][k], sds) {
						unchanged = true
						log.V(2).Info("Secret unchanged; skipping config apply", "next_renewal", nextRenewal)
					} else {
						log.V(0).Info("Updating secret", "next_renewal", nextRenewal)
						r.Config.SecretConfigs[node][k] = sds
						r.Config.IncrementConfigCounter()
					}
				}
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		log.V(2).Info("Adding secret", "next_renewal", nextRenewal)
		if r.Config.SecretConfigs == nil {
			r.Config.SecretConfigs = make(map[string][]*auth.Secret)
		}
		if r.Config.SecretConfigs[nodeid] == nil {
			r.Config.SecretConfigs[nodeid] = []*auth.Secret{}
		}
		r.Config.IncrementConfigCounter()
		r.Config.SecretConfigs[nodeid] = append(r.Config.SecretConfigs[nodeid], sds)
	}
	r.Config.UnlockConfig()

	// Status updates happen outside the lock to minimize lock contention
	var statusNodeID string
	if needsReplace || !found {
		statusNodeID = nodeid
	} else {
		statusNodeID = foundNode
	}

	if statusErr := r.updateTLSSecretStatus(ctx, &tlsSecret, true, []string{statusNodeID}, nextRenewal, certInfo, ""); statusErr != nil {
		log.Error(statusErr, "unable to update TLSSecret status")
	}

	if unchanged {
		return ctrl.Result{
			RequeueAfter: requeueAfter,
		}, nil
	}

	return ctrl.Result{
		RequeueAfter: requeueAfter,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TLSSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Add a Runnable to initialize total count after cache sync
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		log := ctrl.Log.WithName("SDS")

		// Wait for cache to sync
		if !mgr.GetCache().WaitForCacheSync(ctx) {
			return fmt.Errorf("failed to sync cache")
		}

		// Now it's safe to list resources
		var tlsSecretList envoyxdsv1alpha1.TLSSecretList
		if err := r.List(ctx, &tlsSecretList); err != nil {
			return fmt.Errorf("unable to list TLSSecrets: %w", err)
		}

		// Initialize reconciliation status
		count := len(tlsSecretList.Items)
		log.Info("Initializing SDS controller", "resources", count)
		if count > 0 {
			r.Config.ReconciliationStatus.SetHasDomainConfigs(true)
			log.Info("SDS reconciliation starting", "resources", count)
		} else {
			log.Info("SDS reconciliation complete", "resources", 0)
		}
		// Mark domain configs controller as initialized
		r.Config.ReconciliationStatus.SetDomainConfigsInitialized(true)
		return nil
	})); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 10,
		}).
		For(&envoyxdsv1alpha1.TLSSecret{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

// secretsEqual compares two Envoy TLS secrets by name and inline cert/key bytes.
func secretsEqual(a, b *auth.Secret) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Name != b.Name {
		return false
	}
	ac := a.GetTlsCertificate()
	bc := b.GetTlsCertificate()
	if ac == nil || bc == nil {
		return ac == bc
	}
	pubA := ac.GetCertificateChain().GetInlineBytes()
	pubB := bc.GetCertificateChain().GetInlineBytes()
	privA := ac.GetPrivateKey().GetInlineBytes()
	privB := bc.GetPrivateKey().GetInlineBytes()
	return bytes.Equal(pubA, pubB) && bytes.Equal(privA, privB)
}

// extractCertificateInfo extracts certificate information from the secret
func extractCertificateInfo(secret *auth.Secret) *envoyxdsv1alpha1.CertificateInfo {
	if secret == nil {
		return nil
	}

	tlsCert := secret.GetTlsCertificate()
	if tlsCert == nil {
		return nil
	}

	certChain := tlsCert.GetCertificateChain()
	if certChain == nil {
		return nil
	}

	certBytes := certChain.GetInlineBytes()
	if len(certBytes) == 0 {
		return nil
	}

	// Parse the certificate
	block, _ := pem.Decode(certBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil
	}

	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}

	// Calculate fingerprint
	fingerprint := sha256.Sum256(x509Cert.Raw)

	// Determine issuer type
	issuer := x509Cert.Issuer.CommonName
	lowerIssuer := strings.ToLower(issuer)
	switch {
	case strings.Contains(lowerIssuer, "let's encrypt") || strings.Contains(lowerIssuer, "letsencrypt"):
		issuer = "Let's Encrypt"
	case strings.Contains(lowerIssuer, "staging"):
		issuer = "Let's Encrypt (Staging)"
	case x509Cert.Issuer.CommonName == x509Cert.Subject.CommonName:
		issuer = "Self-Signed"
	}

	daysUntilExpiry := int(time.Until(x509Cert.NotAfter).Hours() / 24)
	if daysUntilExpiry < 0 {
		daysUntilExpiry = 0
	}

	return &envoyxdsv1alpha1.CertificateInfo{
		Issuer:          issuer,
		Subject:         x509Cert.Subject.CommonName,
		DNSNames:        x509Cert.DNSNames,
		NotBefore:       metav1.NewTime(x509Cert.NotBefore),
		NotAfter:        metav1.NewTime(x509Cert.NotAfter),
		DaysUntilExpiry: daysUntilExpiry,
		SerialNumber:    x509Cert.SerialNumber.String(),
		Fingerprint:     hex.EncodeToString(fingerprint[:]),
	}
}

// updateTLSSecretStatus updates the status of the TLSSecret CR
func (r *TLSSecretReconciler) updateTLSSecretStatus(ctx context.Context, tlsSecret *envoyxdsv1alpha1.TLSSecret, active bool, activeNodes []string, nextRenewal string, certInfo *envoyxdsv1alpha1.CertificateInfo, message string) error {
	log := ctrllog.FromContext(ctx)

	// Build snapshots info
	snapshots := make([]envoyxdsv1alpha1.SnapshotInfo, 0, len(activeNodes))
	nodesSet := make(map[string]struct{})
	clustersSet := make(map[string]struct{})

	now := metav1.Now()

	for _, nodeID := range activeNodes {
		nodeInfo, _ := util.GetNodeInfo(nodeID) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore

		// Collect unique nodes and clusters
		for _, n := range nodeInfo.Nodes {
			nodesSet[n] = struct{}{}
		}
		for _, c := range nodeInfo.Clusters {
			clustersSet[c] = struct{}{}
		}

		// Build snapshot info
		snapshotInfo := envoyxdsv1alpha1.SnapshotInfo{
			NodeID:      strings.Join(nodeInfo.Nodes, ","),
			Cluster:     strings.Join(nodeInfo.Clusters, ","),
			Active:      true,
			LastUpdated: now,
		}
		snapshots = append(snapshots, snapshotInfo)
	}

	// Convert sets to comma-separated strings
	nodesList := make([]string, 0, len(nodesSet))
	for n := range nodesSet {
		nodesList = append(nodesList, n)
	}
	sort.Strings(nodesList)

	clustersList := make([]string, 0, len(clustersSet))
	for c := range clustersSet {
		clustersList = append(clustersList, c)
	}
	sort.Strings(clustersList)

	// Use retry to handle conflicts when updating status
	tlsSecretKey := types.NamespacedName{Name: tlsSecret.Name, Namespace: tlsSecret.Namespace}
	generation := tlsSecret.Generation

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch the latest version of the TLSSecret to get the current resourceVersion
		var latestTLSSecret envoyxdsv1alpha1.TLSSecret
		if err := r.Get(ctx, tlsSecretKey, &latestTLSSecret); err != nil {
			return err
		}

		// Build conditions from the latest tlsSecret's conditions
		conditions := latestTLSSecret.Status.Conditions
		if conditions == nil {
			conditions = []metav1.Condition{}
		}

		// Update Ready condition
		readyCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.TLSSecretConditionReady,
			LastTransitionTime: now,
			ObservedGeneration: generation,
		}
		if active {
			readyCondition.Status = metav1.ConditionTrue
			readyCondition.Reason = "Active"
			readyCondition.Message = fmt.Sprintf("Secret is active in %d snapshots", len(snapshots))
		} else {
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = "Inactive"
			readyCondition.Message = message
		}
		conditions = updateTLSSecretCondition(conditions, readyCondition)

		// Update Reconciled condition
		reconciledCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.TLSSecretConditionReconciled,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             "Reconciled",
			Message:            "Successfully reconciled",
			ObservedGeneration: generation,
		}
		conditions = updateTLSSecretCondition(conditions, reconciledCondition)

		// Update Error condition if there's an error
		if message != "" && !active {
			errorCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.TLSSecretConditionError,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "Error",
				Message:            message,
				ObservedGeneration: generation,
			}
			conditions = updateTLSSecretCondition(conditions, errorCondition)
		} else {
			// Clear error condition
			errorCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.TLSSecretConditionError,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: now,
				Reason:             "NoError",
				Message:            "",
				ObservedGeneration: generation,
			}
			conditions = updateTLSSecretCondition(conditions, errorCondition)
		}

		// Update CertExpiring condition
		if certInfo != nil && certInfo.DaysUntilExpiry < 30 {
			expiringCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.TLSSecretConditionCertExpiring,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "CertExpiring",
				Message:            fmt.Sprintf("Certificate expires in %d days", certInfo.DaysUntilExpiry),
				ObservedGeneration: generation,
			}
			conditions = updateTLSSecretCondition(conditions, expiringCondition)
		} else {
			expiringCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.TLSSecretConditionCertExpiring,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: now,
				Reason:             "CertValid",
				Message:            "",
				ObservedGeneration: generation,
			}
			conditions = updateTLSSecretCondition(conditions, expiringCondition)
		}

		// Prepare the new status
		newStatus := envoyxdsv1alpha1.TLSSecretStatus{
			Active:             active,
			CertificateInfo:    certInfo,
			NextRenewal:        nextRenewal,
			Snapshots:          snapshots,
			Nodes:              strings.Join(nodesList, ","),
			Clusters:           strings.Join(clustersList, ","),
			LastReconciled:     now,
			ObservedGeneration: generation,
			Conditions:         conditions,
			Message:            message,
		}

		// Update status if changed
		if !tlsSecretStatusEqual(latestTLSSecret.Status, newStatus) {
			latestTLSSecret.Status = newStatus
			if err := r.Status().Update(ctx, &latestTLSSecret); err != nil {
				return err
			}
			log.V(2).Info("Updated tlssecret status", "active", active, "nodes", strings.Join(nodesList, ","))
		}

		return nil
	})
}

// updateTLSSecretCondition updates or adds a condition to the conditions slice
func updateTLSSecretCondition(conditions []metav1.Condition, newCondition metav1.Condition) []metav1.Condition {
	for i, c := range conditions {
		if c.Type == newCondition.Type {
			// Only update LastTransitionTime if status changed
			if c.Status != newCondition.Status {
				conditions[i] = newCondition
			} else {
				// Keep the existing LastTransitionTime
				newCondition.LastTransitionTime = c.LastTransitionTime
				conditions[i] = newCondition
			}
			return conditions
		}
	}
	return append(conditions, newCondition)
}

// tlsSecretStatusEqual compares two TLSSecretStatus objects (ignoring LastReconciled time for comparison)
func tlsSecretStatusEqual(a, b envoyxdsv1alpha1.TLSSecretStatus) bool {
	if a.Active != b.Active {
		return false
	}
	if a.Nodes != b.Nodes {
		return false
	}
	if a.Clusters != b.Clusters {
		return false
	}
	if a.ObservedGeneration != b.ObservedGeneration {
		return false
	}
	if len(a.Snapshots) != len(b.Snapshots) {
		return false
	}
	// Compare certificate info
	if (a.CertificateInfo == nil) != (b.CertificateInfo == nil) {
		return false
	}
	if a.CertificateInfo != nil && b.CertificateInfo != nil {
		if a.CertificateInfo.DaysUntilExpiry != b.CertificateInfo.DaysUntilExpiry {
			return false
		}
		if a.CertificateInfo.Fingerprint != b.CertificateInfo.Fingerprint {
			return false
		}
	}
	return true
}
