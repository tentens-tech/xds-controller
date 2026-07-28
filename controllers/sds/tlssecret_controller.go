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

	"github.com/go-logr/logr"

	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	xdserr "github.com/tentens-tech/xds-controller/pkg/xds/err"
	xdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types"
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
		r.removeSecretFromConfigs(log, req.Name)
		return ctrl.Result{}, nil
	}

	// Suspended by the operator: leave the existing snapshot entry alone and do
	// not touch storage or ACME. Removing the annotation triggers a new
	// reconcile via the annotation-change predicate.
	if IsPaused(&tlsSecret) {
		log.V(0).Info("TLSSecret is paused, skipping certificate operations",
			"annotation", AnnotationPause)
		if statusErr := r.updateTLSSecretStatus(ctx, &tlsSecret, tlsSecretStatusPatch{
			paused:           true,
			message:          "Paused by " + AnnotationPause + " annotation",
			preserveExisting: true,
			preserveBackoff:  true,
		}); statusErr != nil {
			log.Error(statusErr, "unable to update TLSSecret status")
		}
		return ctrl.Result{}, nil
	}

	policy := RetryPolicyFor(&tlsSecret, r.retryDefaults())
	forceRenew := IsForceRenew(&tlsSecret)
	forceRenewRequest := ForceRenewRequest(&tlsSecret)

	// A force-renew annotation whose value differs from the one already acted on
	// is a fresh request, so it clears any pending backoff. An unchanged value
	// stays subject to the backoff, otherwise a stale annotation would retry on
	// every incoming watch event.
	//
	// It also gates the re-issue itself. status.forceRenewRequest is recorded
	// once a new certificate has actually been stored, so if the follow-up
	// patch that removes the annotation fails, the next reconcile retries only
	// that patch. Keying off the annotation alone would place a fresh ACME
	// order on every reconcile until the patch happened to succeed.
	newForceRenewRequest := forceRenew && forceRenewRequest != tlsSecret.Status.ForceRenewRequest

	if wait, inBackoff := backoffRemaining(&tlsSecret); inBackoff && !newForceRenewRequest {
		log.V(1).Info("waiting out backoff after previous failure",
			"failureCount", tlsSecret.Status.FailureCount,
			"retryIn", wait.Truncate(time.Second).String())
		return ctrl.Result{RequeueAfter: wait}, nil
	}

	tlsSecret.Spec.SecretName = req.Name
	sds, renewIn, expiration, err := GetSecret(reconcileCtx, &tlsSecret.Spec.DomainConfig, r.Config, newForceRenewRequest)
	nextRenewal := formatRenewIn(renewIn)

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

	requeueAfter := r.requeueInterval(renewIn)

	// If we're not the leader and there's no certificate, just requeue. This is
	// not our failure, so it does not count towards the backoff.
	if !r.Config.IsLeaderInstance() && (errors.Is(err, xdserr.ErrCertNotFound) || errors.Is(err, xdserr.ErrCertNil)) {
		log.V(2).Info("waiting for leader to generate certificate")
		return ctrl.Result{
			RequeueAfter: requeueAfter,
		}, nil
	}

	if err != nil {
		failureCount := tlsSecret.Status.FailureCount + 1
		retryIn := policy.DelayFor(failureCount)
		now := metav1.Now()
		backoffUntil := metav1.NewTime(now.Add(retryIn))

		log.V(0).Error(err, "Error encountered while creating the secret. Please verify your SDS configuration. The secret will not be added.",
			"failureCount", failureCount,
			"retryIn", retryIn.String(),
			"nextAttempt", backoffUntil.Format(time.RFC3339))
		// The error label must stay low cardinality: it is a Prometheus label,
		// so anything varying per reconcile (timestamps, remaining durations)
		// would create a new time series every time.
		xds.RecordConfigError(tlsSecret.Spec.SecretName, "SDS", errorClass(err))

		if statusErr := r.updateTLSSecretStatus(ctx, &tlsSecret, tlsSecretStatusPatch{
			// nextRenewal is deliberately left empty: on failure the returned
			// duration is the fallback retry interval, not a renewal schedule,
			// so the value from the last successful read is kept instead.
			message:      err.Error(),
			failureCount: failureCount,
			lastFailure:  &now,
			backoffUntil: &backoffUntil,
			nextRetry:    retryIn.String(),
			// Keep the previously observed certificate details: the old
			// certificate is still what Envoy serves, so blanking the issuer,
			// expiry and node columns would misreport the actual state.
			preserveExisting:  true,
			forceRenewRequest: forceRenewRequest,
		}); statusErr != nil {
			log.Error(statusErr, "unable to update TLSSecret status")
		}
		return ctrl.Result{
			RequeueAfter: retryIn,
		}, nil
	}

	// Extract certificate info for status
	certInfo := extractCertificateInfo(sds)

	// servedNow: this reconcile acted on the request and a different
	// certificate is now in place. A dry run or a no-op renewal does not count,
	// so the request is retried rather than silently dropped.
	servedNow := newForceRenewRequest && certificateChanged(tlsSecret.Status.CertificateInfo, certInfo)
	// alreadyServed: an earlier reconcile issued the certificate but its
	// annotation patch failed, so only the patch is still outstanding.
	alreadyServed := forceRenew && forceRenewRequest == tlsSecret.Status.ForceRenewRequest
	served := servedNow || alreadyServed

	// Clear the force-renew annotation only once the request has been served.
	if forceRenew && served {
		if patchErr := r.clearForceRenew(ctx, &tlsSecret); patchErr != nil {
			// The annotation stays; the next reconcile retries the patch alone,
			// without placing another ACME order.
			log.Error(patchErr, "failed to clear force-renew annotation, will retry")
		} else {
			fingerprint := ""
			if certInfo != nil {
				fingerprint = certInfo.Fingerprint
			}
			log.V(0).Info("force renew completed, annotation removed",
				"fingerprint", fingerprint)
			forceRenewRequest = ""
		}
	}

	// Only a served request is recorded, so a dry run or a failed issue does
	// not consume it.
	if forceRenew && !served {
		forceRenewRequest = tlsSecret.Status.ForceRenewRequest
	}

	sds.Name = tlsSecret.Name

	// Use granular locking for SecretConfigs access to allow parallel reconciliation
	r.Config.LockConfig()
	var found bool
	var foundNode string
	var needsReplace bool
	var unchanged bool

	// Walk every node: the same secret name may have been left behind under a
	// stale node ID, and stopping at the first hit would leak those duplicates.
	for node := range r.Config.SecretConfigs {
		if node == nodeid {
			continue
		}
		if removed := removeSecretByName(r.Config.SecretConfigs, node, tlsSecret.Name); removed > 0 {
			found = true
			foundNode = node
			needsReplace = true
			log.V(0).Info("Replacing secret", "from_node", node, "next_renewal", nextRenewal)
			r.Config.IncrementConfigCounter()
		}
	}

	for k, v := range r.Config.SecretConfigs[nodeid] {
		if v.Name != tlsSecret.Name {
			continue
		}
		found = true
		foundNode = nodeid
		needsReplace = false
		// Avoid reapplying config if secret content has not changed
		if secretsEqual(v, sds) {
			unchanged = true
			log.V(2).Info("Secret unchanged; skipping config apply", "next_renewal", nextRenewal)
		} else {
			log.V(0).Info("Updating secret", "next_renewal", nextRenewal)
			r.Config.SecretConfigs[nodeid][k] = sds
			r.Config.IncrementConfigCounter()
		}
		break
	}

	if needsReplace {
		if r.Config.SecretConfigs[nodeid] == nil {
			r.Config.SecretConfigs[nodeid] = []*auth.Secret{}
		}
		r.Config.SecretConfigs[nodeid] = append(r.Config.SecretConfigs[nodeid], sds)
		r.Config.IncrementConfigCounter()
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

	// Success clears any recorded failure so the next error starts the backoff
	// schedule from the base delay again.
	if statusErr := r.updateTLSSecretStatus(ctx, &tlsSecret, tlsSecretStatusPatch{
		active:            true,
		activeNodes:       []string{statusNodeID},
		nextRenewal:       nextRenewal,
		certInfo:          certInfo,
		forceRenewRequest: forceRenewRequest,
	}); statusErr != nil {
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

// retryDefaults returns the controller-wide backoff schedule, falling back to
// the package defaults when the configuration leaves a value unset.
func (r *TLSSecretReconciler) retryDefaults() RetryPolicy {
	return RetryPolicy{
		BaseDelay:  r.Config.RetryBaseDelay,
		MaxDelay:   r.Config.RetryMaxDelay,
		Multiplier: r.Config.RetryMultiplier,
	}
}

// requeueInterval caps the requeue delay at StatusRefreshInterval. Renewal can
// be up to two months away, and requeueing that far out would leave the status
// fields (days until expiry, last reconciled) untouched for the same period.
func (r *TLSSecretReconciler) requeueInterval(renewIn time.Duration) time.Duration {
	refresh := r.Config.StatusRefreshInterval
	if refresh <= 0 {
		refresh = time.Hour
	}
	if renewIn <= 0 {
		return 30 * time.Second
	}
	if renewIn > refresh {
		return refresh
	}
	return renewIn
}

// backoffRemaining reports how long is left of a persisted backoff window.
func backoffRemaining(tlsSecret *envoyxdsv1alpha1.TLSSecret) (time.Duration, bool) {
	until := tlsSecret.Status.BackoffUntil
	if until == nil || until.IsZero() {
		return 0, false
	}
	wait := time.Until(until.Time)
	if wait <= 0 {
		return 0, false
	}
	return wait, true
}

// formatRenewIn renders the time left before renewal for the status field.
func formatRenewIn(renewIn time.Duration) string {
	if renewIn <= 0 {
		return "0d 0h 0m"
	}
	days := int(renewIn.Hours()) / 24
	hours := int(renewIn.Hours()) % 24
	minutes := int(renewIn.Minutes()) % 60
	return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
}

// certificateChanged reports whether a different certificate is now in place.
func certificateChanged(old, current *envoyxdsv1alpha1.CertificateInfo) bool {
	if current == nil {
		return false
	}
	if old == nil {
		return true
	}
	return old.Fingerprint != current.Fingerprint
}

// errorClass maps an error to one of a bounded set of strings. The result is
// used as a Prometheus label, so it must not contain anything that varies per
// occurrence.
func errorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, xdserr.ErrRateLimited):
		return "rate limited"
	case errors.Is(err, xdserr.ErrServiceBusy):
		return "service busy"
	case errors.Is(err, xdserr.ErrCertNotFound):
		return "cert not found"
	case errors.Is(err, xdserr.ErrCertNil):
		return "cert nil"
	case errors.Is(err, xdserr.ErrBadKeyData):
		return "bad key data"
	case errors.Is(err, xdserr.ErrVaultNotConfigured):
		return "vault not configured"
	case errors.Is(err, xdserr.ErrK8sClientNotConfigured):
		return "kubernetes client not configured"
	case errors.Is(err, xdserr.ErrLetsEncryptAccount):
		return "lets encrypt account invalid"
	case errors.Is(err, xdserr.ErrCustomEnvReplace):
		return "custom env replace"
	case strings.Contains(err.Error(), xdserr.ErrWriteCert.Error()):
		return "write cert"
	case strings.Contains(err.Error(), xdserr.ErrReadCert.Error()):
		return "read cert"
	case strings.Contains(err.Error(), xdserr.ErrGetCert.Error()):
		return "get cert"
	default:
		return "other"
	}
}

// clearForceRenew removes the force-renew annotation after a successful re-issue.
func (r *TLSSecretReconciler) clearForceRenew(ctx context.Context, tlsSecret *envoyxdsv1alpha1.TLSSecret) error {
	old := tlsSecret.DeepCopy()
	delete(tlsSecret.Annotations, AnnotationForceRenew)
	delete(tlsSecret.Labels, AnnotationForceRenew)
	if tlsSecret.Annotations == nil {
		tlsSecret.Annotations = make(map[string]string)
	}
	tlsSecret.Annotations[AnnotationForceRenewedAt] = time.Now().UTC().Format(time.RFC3339)
	return r.Patch(ctx, tlsSecret, client.MergeFrom(old))
}

// removeSecretFromConfigs drops every copy of a secret from all node snapshots.
func (r *TLSSecretReconciler) removeSecretFromConfigs(log logr.Logger, name string) {
	r.Config.LockConfig()
	defer r.Config.UnlockConfig()

	for node := range r.Config.SecretConfigs {
		if removed := removeSecretByName(r.Config.SecretConfigs, node, name); removed > 0 {
			log.V(0).Info("Removing secret", "node", node, "removed", removed)
			r.Config.IncrementConfigCounter()
		}
	}
}

// removeSecretByName filters out every secret with the given name from one
// node's slice and returns how many were dropped. Filtering in place rather
// than deleting during a range avoids skipping entries when the slice shifts.
func removeSecretByName(configs map[string][]*auth.Secret, node, name string) int {
	secrets := configs[node]
	// Build a new slice rather than compacting in place: the previous slice may
	// still be referenced by an in-flight snapshot.
	kept := make([]*auth.Secret, 0, len(secrets))
	for _, s := range secrets {
		if s.Name == name {
			continue
		}
		kept = append(kept, s)
	}
	removed := len(secrets) - len(kept)
	if removed > 0 {
		configs[node] = kept
	}
	return removed
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
		// Annotation and label changes do not bump metadata.generation, so a
		// generation-only predicate would make envoyxds.io/force-renew,
		// envoyxds.io/pause and the retry overrides invisible to the controller.
		For(&envoyxdsv1alpha1.TLSSecret{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
			predicate.LabelChangedPredicate{},
		))).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findTLSSecretsForK8sSecret),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

// resolveK8sSecretRef returns the Kubernetes Secret name and namespace that a TLSSecret
// references for its certificate storage. It returns empty strings if the TLSSecret does
// not use Kubernetes storage. The globalStorage parameter provides the controller-level
// storage config so that the inference matches GetSecret (e.g. global Vault config).
func resolveK8sSecretRef(tlsSecret *envoyxdsv1alpha1.TLSSecret, defaultNamespace string, globalStorage *xdstypes.StorageConfig) (name, namespace string) {
	spec := &tlsSecret.Spec.DomainConfig

	storageType := spec.Config.Type

	// When Type is not explicitly set, infer it the same way GetSecret does:
	// if KubernetesStorageConfig is present, or if no local path is set, it
	// defaults to Kubernetes storage.
	if storageType == "" {
		switch {
		case spec.Challenge != nil:
			// Let's Encrypt: defaults to Kubernetes when no Vault is configured.
			// Check both per-resource and global Vault config, matching GetSecret.
			if spec.Config.VaultStorageConfig != nil {
				return "", ""
			}
			if globalStorage != nil && globalStorage.VaultStorageConfig != nil {
				return "", ""
			}
			storageType = xdstypes.Kubernetes
		case spec.Config.KubernetesStorageConfig != nil:
			storageType = xdstypes.Kubernetes
		case spec.Config.LocalStorageConfig != nil && spec.Config.LocalStorageConfig.Path != "":
			return "", ""
		default:
			// Falls back to Kubernetes when k8s client is available (which it is
			// if this controller is running), matching GetSecret behavior.
			storageType = xdstypes.Kubernetes
		}
	}

	if storageType != xdstypes.Kubernetes {
		return "", ""
	}

	// Resolve effective secret name and namespace, mirroring StorageConfigRead logic.
	name = tlsSecret.Name
	namespace = defaultNamespace
	if tlsSecret.Namespace != "" {
		namespace = tlsSecret.Namespace
	}
	if spec.Config.KubernetesStorageConfig != nil {
		if spec.Config.KubernetesStorageConfig.SecretName != "" {
			name = spec.Config.KubernetesStorageConfig.SecretName
		}
		if spec.Config.KubernetesStorageConfig.Namespace != "" {
			namespace = spec.Config.KubernetesStorageConfig.Namespace
		}
	}
	return name, namespace
}

// findTLSSecretsForK8sSecret maps a Kubernetes Secret event to the TLSSecret
// resources that reference it, so that updating a K8s Secret triggers
// reconciliation of the corresponding TLSSecret(s).
func (r *TLSSecretReconciler) findTLSSecretsForK8sSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)

	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	var tlsSecretList envoyxdsv1alpha1.TLSSecretList
	if err := r.List(ctx, &tlsSecretList); err != nil {
		log.Error(err, "Failed to list TLSSecrets for Secret watch")
		return nil
	}

	var requests []reconcile.Request
	for i := range tlsSecretList.Items {
		ts := &tlsSecretList.Items[i]
		refName, refNamespace := resolveK8sSecretRef(ts, r.Config.DefaultNamespace, &r.Config.Storage)
		if refName == "" {
			continue
		}
		if refName == secret.Name && refNamespace == secret.Namespace {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      ts.Name,
					Namespace: ts.Namespace,
				},
			})
		}
	}

	if len(requests) > 0 {
		log.V(1).Info("Kubernetes Secret changed, triggering TLSSecret reconciliation",
			"secret", secret.Name, "namespace", secret.Namespace, "tlsSecrets", len(requests))
	}

	return requests
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

// tlsSecretStatusPatch carries everything a single reconcile wants to record on
// the TLSSecret status.
type tlsSecretStatusPatch struct {
	active      bool
	activeNodes []string
	nextRenewal string
	certInfo    *envoyxdsv1alpha1.CertificateInfo
	message     string
	paused      bool

	// Failure bookkeeping. Zero values reset the backoff, which is what a
	// successful reconcile wants.
	failureCount int32
	lastFailure  *metav1.Time
	backoffUntil *metav1.Time
	nextRetry    string

	// forceRenewRequest is the force-renew annotation value acted on.
	forceRenewRequest string

	// preserveExisting keeps the certificate details, nodes, clusters and
	// snapshots recorded by the last successful reconcile. Used when the
	// reconcile failed or is paused: the previously issued certificate is still
	// what Envoy serves, so reporting it as unknown would be wrong.
	preserveExisting bool

	// preserveBackoff keeps the recorded failure state instead of resetting it.
	// A pause must not clear the backoff, otherwise pausing and unpausing a
	// rate-limited secret would restart the retries immediately.
	preserveBackoff bool
}

// minStatusHeartbeat keeps the heartbeat from collapsing to zero if the refresh
// interval is configured very low.
const minStatusHeartbeat = 5 * time.Second

// statusHeartbeat bounds how stale status.lastReconciled may get. Without it a
// reconcile that changes nothing never refreshes the timestamp, and the field
// reads as if the controller had stopped working. It tracks the reconcile
// cadence (slightly under it, so a scheduled wake-up reliably counts as due)
// rather than a fixed value, so the timestamp is refreshed once per scheduled
// reconcile and no more often.
func (r *TLSSecretReconciler) statusHeartbeat() time.Duration {
	refresh := r.Config.StatusRefreshInterval
	if refresh <= 0 {
		refresh = time.Hour
	}
	heartbeat := refresh - refresh/10
	if heartbeat < minStatusHeartbeat {
		heartbeat = minStatusHeartbeat
	}
	return heartbeat
}

// updateTLSSecretStatus updates the status of the TLSSecret CR
func (r *TLSSecretReconciler) updateTLSSecretStatus(ctx context.Context, tlsSecret *envoyxdsv1alpha1.TLSSecret, patch tlsSecretStatusPatch) error {
	log := ctrllog.FromContext(ctx)

	active := patch.active
	activeNodes := patch.activeNodes
	nextRenewal := patch.nextRenewal
	certInfo := patch.certInfo
	message := patch.message

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

		// Effective values for this write. They are locals rather than the
		// captured outer variables, so a conflict retry recomputes them from the
		// re-fetched object instead of compounding the previous attempt.
		effActive := active
		effCertInfo := certInfo
		effSnapshots := snapshots
		effNodes := nodesList
		effClusters := clustersList
		effNextRenewal := nextRenewal

		// Carry forward what the last successful reconcile observed. A failed or
		// paused reconcile did not invalidate the certificate Envoy is serving,
		// so blanking these fields would misreport the actual state.
		if patch.preserveExisting {
			if effCertInfo == nil {
				effCertInfo = latestTLSSecret.Status.CertificateInfo
			}
			if len(effSnapshots) == 0 {
				effSnapshots = latestTLSSecret.Status.Snapshots
				effNodes = util.ParseCSV(latestTLSSecret.Status.Nodes)
				effClusters = util.ParseCSV(latestTLSSecret.Status.Clusters)
			}
			if effNextRenewal == "" {
				effNextRenewal = latestTLSSecret.Status.NextRenewal
			}
			// A secret already published to a snapshot keeps serving while the
			// controller retries in the background.
			effActive = effActive || latestTLSSecret.Status.Active
		}

		effFailureCount := patch.failureCount
		effLastFailure := patch.lastFailure
		effBackoffUntil := patch.backoffUntil
		effNextRetry := patch.nextRetry
		effForceRenewRequest := patch.forceRenewRequest
		if patch.preserveBackoff {
			effFailureCount = latestTLSSecret.Status.FailureCount
			effLastFailure = latestTLSSecret.Status.LastFailureTime
			effBackoffUntil = latestTLSSecret.Status.BackoffUntil
			effNextRetry = latestTLSSecret.Status.NextRetryDelay
			effForceRenewRequest = latestTLSSecret.Status.ForceRenewRequest
		}

		failed := patch.failureCount > 0

		// Update Ready condition
		readyCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.TLSSecretConditionReady,
			LastTransitionTime: now,
			ObservedGeneration: generation,
		}
		switch {
		case effActive:
			readyCondition.Status = metav1.ConditionTrue
			readyCondition.Reason = "Active"
			readyCondition.Message = fmt.Sprintf("Secret is active in %d snapshots", len(effSnapshots))
		default:
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

		// Update Paused condition
		pausedCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.TLSSecretConditionPaused,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             "NotPaused",
			ObservedGeneration: generation,
		}
		if patch.paused {
			pausedCondition.Status = metav1.ConditionTrue
			pausedCondition.Reason = "Paused"
			pausedCondition.Message = message
		}
		conditions = updateTLSSecretCondition(conditions, pausedCondition)

		// Update the Error condition. A paused reconcile carries a message too,
		// but it is not a failure and it did not re-check anything, so it leaves
		// whatever the last real attempt recorded in place.
		if !patch.preserveBackoff {
			errorCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.TLSSecretConditionError,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: now,
				Reason:             "NoError",
				ObservedGeneration: generation,
			}
			if failed {
				errorCondition.Status = metav1.ConditionTrue
				errorCondition.Reason = "Error"
				errorCondition.Message = message
			}
			conditions = updateTLSSecretCondition(conditions, errorCondition)
		}

		// Update CertExpiring condition
		if effCertInfo != nil && effCertInfo.DaysUntilExpiry < 30 {
			expiringCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.TLSSecretConditionCertExpiring,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "CertExpiring",
				Message:            fmt.Sprintf("Certificate expires in %d days", effCertInfo.DaysUntilExpiry),
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
			Active:             effActive,
			CertificateInfo:    effCertInfo,
			NextRenewal:        effNextRenewal,
			Snapshots:          effSnapshots,
			Nodes:              strings.Join(effNodes, ","),
			Clusters:           strings.Join(effClusters, ","),
			LastReconciled:     now,
			ObservedGeneration: generation,
			Conditions:         conditions,
			Message:            message,
			FailureCount:       effFailureCount,
			LastFailureTime:    effLastFailure,
			BackoffUntil:       effBackoffUntil,
			NextRetryDelay:     effNextRetry,
			Paused:             patch.paused,
			ForceRenewRequest:  effForceRenewRequest,
		}

		// Update status if anything other than the timestamp changed, or if the
		// timestamp has gone stale. Without the heartbeat, status.lastReconciled
		// would freeze on a healthy secret and read as a stalled controller.
		stale := time.Since(latestTLSSecret.Status.LastReconciled.Time) >= r.statusHeartbeat()
		if stale || !tlsSecretStatusEqual(latestTLSSecret.Status, newStatus) {
			latestTLSSecret.Status = newStatus
			if err := r.Status().Update(ctx, &latestTLSSecret); err != nil {
				return err
			}
			log.V(2).Info("Updated tlssecret status", "active", effActive, "nodes", strings.Join(effNodes, ","))
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

// tlsSecretStatusEqual compares two TLSSecretStatus objects, ignoring the
// timestamps that move on every reconcile: LastReconciled and each snapshot's
// LastUpdated. Everything else is compared, so a change to nextRenewal, the
// message, a condition or the backoff state does result in a write. Comparing
// only a subset was why status fields such as nextRenewal and lastReconciled
// went stale for weeks at a time.
func tlsSecretStatusEqual(a, b envoyxdsv1alpha1.TLSSecretStatus) bool {
	return apiequality.Semantic.DeepEqual(normalizeStatusForCompare(a), normalizeStatusForCompare(b))
}

// normalizeStatusForCompare zeroes the fields that are expected to change on
// every reconcile so they do not by themselves trigger a write.
func normalizeStatusForCompare(s envoyxdsv1alpha1.TLSSecretStatus) envoyxdsv1alpha1.TLSSecretStatus {
	out := *s.DeepCopy()
	out.LastReconciled = metav1.Time{}
	for i := range out.Snapshots {
		out.Snapshots[i].LastUpdated = metav1.Time{}
	}
	for i := range out.Conditions {
		out.Conditions[i].LastTransitionTime = metav1.Time{}
	}
	// BackoffUntil moves with every failure; the failure count already captures
	// whether anything meaningful changed, and comparing the exact instant would
	// force a write on each no-op retry.
	out.BackoffUntil = nil
	out.LastFailureTime = nil
	return out
}
