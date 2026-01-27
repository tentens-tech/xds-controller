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

// Package eds implements the EDS (Endpoint Discovery Service) controller.
package eds

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3" // registers trace types

	// Health checkers (endpoints may have health check configs)
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/health_checkers/redis/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/health_checkers/thrift/v3"

	// Tracers
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/resource_detectors/v3"

	// Transport sockets (for endpoint-level transport config)
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"google.golang.org/protobuf/encoding/protojson"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	edstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/eds"
)

// EndpointReconciler reconciles a Endpoint object
type EndpointReconciler struct {
	client.Client
	Scheme                 *runtime.Scheme
	Config                 *xds.Config
	reconciling            atomic.Int32
	lastReconcileTime      atomic.Int64
	initialReconcileLogged atomic.Bool
}

//+kubebuilder:rbac:groups=envoyxds.io,resources=endpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=envoyxds.io,resources=endpoints/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=envoyxds.io,resources=endpoints/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *EndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	r.Config.LockConfig()
	defer r.Config.UnlockConfig()

	// Mark that we have endpoints to reconcile (handles dynamically added resources)
	r.Config.ReconciliationStatus.SetHasEndpoints(true)
	r.reconciling.Add(1)
	r.lastReconcileTime.Store(time.Now().UnixNano())

	// Create a child context with timeout
	reconcileCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	go func() {
		defer func() {
			time.Sleep(time.Second)
			count := r.reconciling.Add(-1)
			if count == 0 {
				r.Config.ReconciliationStatus.SetEndpointsReconciled(true)
				// Log only once when initial reconciliation completes
				if !r.initialReconcileLogged.Swap(true) {
					ctrl.Log.WithName("EDS").Info("EDS reconciliation complete")
				}
			}
		}()

		<-reconcileCtx.Done()
		if reconcileCtx.Err() == context.DeadlineExceeded {
			log.Info("Reconciliation timed out")
		}
	}()

	var ep envoyxdsv1alpha1.Endpoint
	endpointConfigFound := true
	if err := r.Get(ctx, req.NamespacedName, &ep); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to get endpoint")
			return ctrl.Result{}, err
		}
		endpointConfigFound = false
	}

	// Set default nodes and clusters if not present
	if ep.Annotations == nil {
		ep.Annotations = make(map[string]string)
	}
	if ep.Annotations["nodes"] == "" && endpointConfigFound {
		ep.Annotations["nodes"] = r.Config.NodeID
	}
	if ep.Annotations["clusters"] == "" && endpointConfigFound {
		ep.Annotations["clusters"] = r.Config.Cluster
	}
	nodeid := util.GetNodeID(ep.Annotations)

	if !endpointConfigFound {
		for node := range r.Config.EndpointConfigs {
			for i, e := range r.Config.EndpointConfigs[node] {
				if e.ClusterName == req.Name {
					log.V(0).Info("Removed endpoint")
					r.Config.EndpointConfigs[node] = append(r.Config.EndpointConfigs[node][:i], r.Config.EndpointConfigs[node][i+1:]...)
					r.Config.IncrementConfigCounter()
				}
			}
		}
		return ctrl.Result{}, nil
	}

	eds, err := EndpointRecast(ep.Spec.EDS)
	if err != nil {
		log.Error(err, "unable to recast endpoint")
		xds.RecordConfigError(ep.Name, "EDS", err.Error())
		// Update status with error
		if statusErr := r.updateEndpointStatus(ctx, &ep, false, nil, 0, err.Error()); statusErr != nil {
			log.Error(statusErr, "unable to update Endpoint status")
		}
		return ctrl.Result{}, err
	}

	// Use resource name as cluster_name if not specified
	if eds.ClusterName == "" {
		eds.ClusterName = ep.Name
	}

	// Count total endpoints
	endpointCount := countEndpoints(eds)

	for node := range r.Config.EndpointConfigs {
		for k, v := range r.Config.EndpointConfigs[node] {
			if v.ClusterName == eds.ClusterName {
				if nodeid != node {
					log.V(0).Info("Replacing endpoint")
					r.Config.EndpointConfigs[node] = append(r.Config.EndpointConfigs[node][:k], r.Config.EndpointConfigs[node][k+1:]...)
					if r.Config.EndpointConfigs[nodeid] == nil {
						r.Config.EndpointConfigs[nodeid] = []*endpoint.ClusterLoadAssignment{}
					}
					r.Config.EndpointConfigs[nodeid] = append(r.Config.EndpointConfigs[nodeid], eds)
					r.Config.IncrementConfigCounter()
					// Update status
					if statusErr := r.updateEndpointStatus(ctx, &ep, true, []string{nodeid}, endpointCount, ""); statusErr != nil {
						log.Error(statusErr, "unable to update Endpoint status")
					}
					return ctrl.Result{}, nil
				} else {
					log.V(0).Info("Updating endpoint")
					r.Config.EndpointConfigs[node][k] = eds
					r.Config.IncrementConfigCounter()
					// Update status
					if statusErr := r.updateEndpointStatus(ctx, &ep, true, []string{node}, endpointCount, ""); statusErr != nil {
						log.Error(statusErr, "unable to update Endpoint status")
					}
					return ctrl.Result{}, nil
				}
			}
		}
	}

	log.V(2).Info("Adding endpoint")
	if r.Config.EndpointConfigs == nil {
		r.Config.EndpointConfigs = make(map[string][]*endpoint.ClusterLoadAssignment)
	}
	if r.Config.EndpointConfigs[nodeid] == nil {
		r.Config.EndpointConfigs[nodeid] = []*endpoint.ClusterLoadAssignment{}
	}
	r.Config.EndpointConfigs[nodeid] = append(r.Config.EndpointConfigs[nodeid], eds)
	r.Config.IncrementConfigCounter()
	// Update status
	if statusErr := r.updateEndpointStatus(ctx, &ep, true, []string{nodeid}, endpointCount, ""); statusErr != nil {
		log.Error(statusErr, "unable to update Endpoint status")
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Add a Runnable to initialize total count after cache sync
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		log := ctrl.Log.WithName("EDS")

		// Wait for cache to sync
		if !mgr.GetCache().WaitForCacheSync(ctx) {
			return fmt.Errorf("failed to sync cache")
		}

		// Now it's safe to list resources
		var endpointList envoyxdsv1alpha1.EndpointList
		if err := r.List(ctx, &endpointList); err != nil {
			return fmt.Errorf("unable to list Endpoints: %w", err)
		}

		// Initialize reconciliation status
		count := len(endpointList.Items)
		log.Info("Initializing EDS controller", "resources", count)
		if count > 0 {
			r.Config.ReconciliationStatus.SetHasEndpoints(true)
			log.Info("EDS reconciliation starting", "resources", count)
		} else {
			log.Info("EDS reconciliation complete", "resources", 0)
		}
		// Mark endpoints controller as initialized
		r.Config.ReconciliationStatus.SetEndpointsInitialized(true)
		return nil
	})); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&envoyxdsv1alpha1.Endpoint{}).
		Complete(r)
}

// EndpointRecast converts EDS types to Envoy ClusterLoadAssignment
func EndpointRecast(e edstypes.EDS) (*endpoint.ClusterLoadAssignment, error) {
	cla := &endpoint.ClusterLoadAssignment{}

	// Marshal EDS to JSON
	edsData, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("JSON marshaling failed: %w", err)
	}

	// Unmarshal JSON to ClusterLoadAssignment
	err = protojson.Unmarshal(edsData, cla)
	if err != nil {
		return nil, fmt.Errorf("proto unmarshaling failed: %w", err)
	}

	return cla, nil
}

// countEndpoints counts the total number of lb_endpoints
func countEndpoints(cla *endpoint.ClusterLoadAssignment) int {
	count := 0
	if cla == nil || cla.Endpoints == nil {
		return count
	}
	for _, locality := range cla.Endpoints {
		if locality != nil && locality.LbEndpoints != nil {
			count += len(locality.LbEndpoints)
		}
	}
	return count
}

// updateEndpointStatus updates the status of the Endpoint CR
func (r *EndpointReconciler) updateEndpointStatus(ctx context.Context, endpointCR *envoyxdsv1alpha1.Endpoint, active bool, activeNodes []string, endpointCount int, message string) error {
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
	endpointKey := types.NamespacedName{Name: endpointCR.Name, Namespace: endpointCR.Namespace}
	generation := endpointCR.Generation

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch the latest version of the Endpoint to get the current resourceVersion
		var latestEndpoint envoyxdsv1alpha1.Endpoint
		if err := r.Get(ctx, endpointKey, &latestEndpoint); err != nil {
			return err
		}

		// Build conditions from the latest endpoint's conditions
		conditions := latestEndpoint.Status.Conditions
		if conditions == nil {
			conditions = []metav1.Condition{}
		}

		// Update Ready condition
		readyCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.EndpointConditionReady,
			LastTransitionTime: now,
			ObservedGeneration: generation,
		}
		if active {
			readyCondition.Status = metav1.ConditionTrue
			readyCondition.Reason = "Active"
			readyCondition.Message = fmt.Sprintf("Endpoint is active in %d snapshots with %d endpoints", len(snapshots), endpointCount)
		} else {
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = "Inactive"
			readyCondition.Message = message
		}
		conditions = updateCondition(conditions, readyCondition)

		// Update Reconciled condition
		reconciledCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.EndpointConditionReconciled,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             "Reconciled",
			Message:            "Successfully reconciled",
			ObservedGeneration: generation,
		}
		conditions = updateCondition(conditions, reconciledCondition)

		// Update Error condition if there's an error
		if message != "" && !active {
			errorCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.EndpointConditionError,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "Error",
				Message:            message,
				ObservedGeneration: generation,
			}
			conditions = updateCondition(conditions, errorCondition)
		} else {
			// Clear error condition
			errorCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.EndpointConditionError,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: now,
				Reason:             "NoError",
				Message:            "",
				ObservedGeneration: generation,
			}
			conditions = updateCondition(conditions, errorCondition)
		}

		// Prepare the new status
		newStatus := envoyxdsv1alpha1.EndpointStatus{
			Active:             active,
			EndpointCount:      endpointCount,
			Snapshots:          snapshots,
			Nodes:              strings.Join(nodesList, ","),
			Clusters:           strings.Join(clustersList, ","),
			LastReconciled:     now,
			ObservedGeneration: generation,
			Conditions:         conditions,
			Message:            message,
		}

		// Update status if changed
		if !endpointStatusEqual(latestEndpoint.Status, newStatus) {
			latestEndpoint.Status = newStatus
			if err := r.Status().Update(ctx, &latestEndpoint); err != nil {
				return err
			}
			log.V(2).Info("Updated endpoint status", "active", active, "endpointCount", endpointCount, "nodes", strings.Join(nodesList, ","))
		}

		return nil
	})
}

// updateCondition updates or adds a condition to the conditions slice
func updateCondition(conditions []metav1.Condition, newCondition metav1.Condition) []metav1.Condition {
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

// endpointStatusEqual compares two EndpointStatus objects (ignoring LastReconciled time for comparison)
func endpointStatusEqual(a, b envoyxdsv1alpha1.EndpointStatus) bool {
	if a.Active != b.Active {
		return false
	}
	if a.EndpointCount != b.EndpointCount {
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
	return true
}
