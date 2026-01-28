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

// Package cds implements the CDS (Cluster Discovery Service) controller.
package cds

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3" // registers trace types

	// Access loggers
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/grpc/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/open_telemetry/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/stream/v3"

	// Health checkers
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/health_checkers/redis/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/health_checkers/thrift/v3"

	// Load balancing policies
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/least_request/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/maglev/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/random/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/ring_hash/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/round_robin/v3"

	// Retry host predicates
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/omit_canary_hosts/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/omit_host_metadata/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/previous_hosts/v3"

	// Tracers
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/resource_detectors/v3"

	// Transport sockets
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/proxy_protocol/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	// Network
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/network/dns_resolver/cares/v3"

	// Upstreams
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/tcp/v3"
	"google.golang.org/protobuf/encoding/protojson"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	cdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/cds"
	edstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/eds"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Scheme                 *runtime.Scheme
	Config                 *xds.Config
	reconciling            atomic.Int32
	lastReconcileTime      atomic.Int64
	initialReconcileLogged atomic.Bool
}

//+kubebuilder:rbac:groups=envoyxds.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=envoyxds.io,resources=clusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=envoyxds.io,resources=clusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	r.Config.LockConfig()
	defer r.Config.UnlockConfig()

	// Mark that we have clusters to reconcile (handles dynamically added resources)
	r.Config.ReconciliationStatus.SetHasClusters(true)
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
				r.Config.ReconciliationStatus.SetClustersReconciled(true)
				// Log only once when initial reconciliation completes
				if !r.initialReconcileLogged.Swap(true) {
					ctrl.Log.WithName("CDS").Info("CDS reconciliation complete")
				}
			}
		}()

		<-reconcileCtx.Done()
		if reconcileCtx.Err() == context.DeadlineExceeded {
			log.Info("Reconciliation timed out")
		}
	}()

	var cl envoyxdsv1alpha1.Cluster
	clusterConfigFound := true
	if err := r.Get(ctx, req.NamespacedName, &cl); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to get cluster")
			return ctrl.Result{}, err
		}
		clusterConfigFound = false
	}

	// Set default nodes and clusters if not present
	if cl.Annotations == nil {
		cl.Annotations = make(map[string]string)
	}
	if cl.Annotations["nodes"] == "" && clusterConfigFound {
		cl.Annotations["nodes"] = r.Config.NodeID
	}
	if cl.Annotations["clusters"] == "" && clusterConfigFound {
		cl.Annotations["clusters"] = r.Config.Cluster
	}
	nodeid := util.GetNodeID(cl.Annotations)

	if !clusterConfigFound {
		for node := range r.Config.ClusterConfigs {
			for i, c := range r.Config.ClusterConfigs[node] {
				if c.Name == req.Name {
					log.V(0).Info("Removed cluster")
					r.Config.ClusterConfigs[node] = append(r.Config.ClusterConfigs[node][:i], r.Config.ClusterConfigs[node][i+1:]...)
					r.Config.IncrementConfigCounter()
				}
			}
		}
		return ctrl.Result{}, nil
	}

	cds, err := ClusterRecast(cl.Spec.CDS)
	if err != nil {
		log.Error(err, "unable to recast cluster")
		if strings.Contains(err.Error(), "could not resolve Any message type") {
			log.V(0).Info("you need te add import in cds controller to fix this error, example: 	_ \"github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3\"")
		}
		xds.RecordConfigError(cl.Name, "CDS", "you need te add import in cds controller to fix this error, example: 	_ \"github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3\"")
		// Update status with error
		if statusErr := r.updateClusterStatus(ctx, &cl, false, nil, 0, nil, err.Error()); statusErr != nil {
			log.Error(statusErr, "unable to update Cluster status")
		}
		return ctrl.Result{}, err
	}
	cds.Name = cl.Name

	// Check if cluster has inline load_assignment
	hasInlineLoadAssignment := cds.LoadAssignment != nil && len(cds.LoadAssignment.Endpoints) > 0

	// Merge endpoints from referenced Endpoint resources
	endpointCount := 0
	var referencedEndpoints []string
	hasEndpointRefs := len(cl.Spec.EndpointRefs) > 0

	if hasEndpointRefs {
		mergedEndpoints, refs, count, err := r.mergeEndpointRefs(ctx, &cl, cds)
		if err != nil {
			log.Error(err, "unable to merge endpoint refs")
			xds.RecordConfigError(cl.Name, "CDS", "unable to merge endpoint refs: "+err.Error())
			if statusErr := r.updateClusterStatus(ctx, &cl, false, nil, 0, nil, err.Error()); statusErr != nil {
				log.Error(statusErr, "unable to update Cluster status")
			}
			return ctrl.Result{}, err
		}
		if mergedEndpoints != nil && len(mergedEndpoints.Endpoints) > 0 {
			cds.LoadAssignment = mergedEndpoints
		}
		referencedEndpoints = refs
		endpointCount = count
		log.V(1).Info("Merged endpoints from refs", "count", endpointCount, "refs", referencedEndpoints)
	}

	if hasEndpointRefs && !hasInlineLoadAssignment && endpointCount == 0 {
		log.V(0).Info("Cluster uses endpoint_refs but no endpoints found yet, not adding to snapshot", "refs", cl.Spec.EndpointRefs)
		r.removeClusterFromAllNodes(ctx, cl.Name)
		if statusErr := r.updateClusterStatus(ctx, &cl, false, nil, 0, nil, "Waiting for endpoint refs to be available"); statusErr != nil {
			log.Error(statusErr, "unable to update Cluster status")
		}
		return ctrl.Result{}, nil
	}

	for node := range r.Config.ClusterConfigs {
		for k, v := range r.Config.ClusterConfigs[node] {
			if v.Name == cl.Name {
				if nodeid != node {
					log.V(0).Info("Replacing cluster")
					r.Config.ClusterConfigs[node] = append(r.Config.ClusterConfigs[node][:k], r.Config.ClusterConfigs[node][k+1:]...)
					if r.Config.ClusterConfigs[nodeid] == nil {
						r.Config.ClusterConfigs[nodeid] = []*cluster.Cluster{}
					}
					r.Config.ClusterConfigs[nodeid] = append(r.Config.ClusterConfigs[nodeid], cds)
					r.Config.IncrementConfigCounter()
					// Update status
					if statusErr := r.updateClusterStatus(ctx, &cl, true, []string{nodeid}, endpointCount, referencedEndpoints, ""); statusErr != nil {
						log.Error(statusErr, "unable to update Cluster status")
					}
					return ctrl.Result{}, nil
				} else {
					log.V(0).Info("Updating cluster")
					r.Config.ClusterConfigs[node][k] = cds
					r.Config.IncrementConfigCounter()
					// Update status
					if statusErr := r.updateClusterStatus(ctx, &cl, true, []string{node}, endpointCount, referencedEndpoints, ""); statusErr != nil {
						log.Error(statusErr, "unable to update Cluster status")
					}
					return ctrl.Result{}, nil
				}
			}
		}
	}

	log.V(2).Info("Adding cluster")
	if r.Config.ClusterConfigs == nil {
		r.Config.ClusterConfigs = make(map[string][]*cluster.Cluster)
	}
	if r.Config.ClusterConfigs[nodeid] == nil {
		r.Config.ClusterConfigs[nodeid] = []*cluster.Cluster{}
	}
	r.Config.ClusterConfigs[nodeid] = append(r.Config.ClusterConfigs[nodeid], cds)
	r.Config.IncrementConfigCounter()
	// Update status
	if statusErr := r.updateClusterStatus(ctx, &cl, true, []string{nodeid}, endpointCount, referencedEndpoints, ""); statusErr != nil {
		log.Error(statusErr, "unable to update Cluster status")
	}
	return ctrl.Result{}, nil
}

// mergeEndpointRefs fetches and merges endpoints from referenced Endpoint resources
func (r *ClusterReconciler) mergeEndpointRefs(ctx context.Context, cl *envoyxdsv1alpha1.Cluster, cds *cluster.Cluster) (loadAssignment *endpoint.ClusterLoadAssignment, refs []string, count int, err error) {
	log := ctrllog.FromContext(ctx)

	if len(cl.Spec.EndpointRefs) == 0 {
		return nil, nil, 0, nil
	}

	// Initialize load assignment
	loadAssignment = &endpoint.ClusterLoadAssignment{
		ClusterName: cl.Name,
	}

	// If cluster already has load_assignment, use it as base
	if cds.LoadAssignment != nil {
		loadAssignment = cds.LoadAssignment
		if loadAssignment.ClusterName == "" {
			loadAssignment.ClusterName = cl.Name
		}
	}

	referencedEndpoints := make([]string, 0, len(cl.Spec.EndpointRefs))
	totalEndpointCount := 0

	for _, epRef := range cl.Spec.EndpointRefs {
		var ep envoyxdsv1alpha1.Endpoint
		if err := r.Get(ctx, types.NamespacedName{Name: epRef, Namespace: cl.Namespace}, &ep); err != nil {
			if apierrors.IsNotFound(err) {
				log.V(1).Info("Referenced endpoint not found", "endpoint", epRef)
				continue
			}
			return nil, nil, 0, fmt.Errorf("failed to get endpoint %s: %w", epRef, err)
		}

		referencedEndpoints = append(referencedEndpoints, epRef)

		// Convert EDS types to Envoy types
		edsEndpoints, err := EndpointRecast(ep.Spec.EDS)
		if err != nil {
			log.Error(err, "unable to recast endpoint", "endpoint", epRef)
			continue
		}

		// Merge endpoints
		if edsEndpoints.Endpoints != nil {
			loadAssignment.Endpoints = append(loadAssignment.Endpoints, edsEndpoints.Endpoints...)
			for _, locality := range edsEndpoints.Endpoints {
				if locality != nil && locality.LbEndpoints != nil {
					totalEndpointCount += len(locality.LbEndpoints)
				}
			}
		}

		// Merge named endpoints
		if edsEndpoints.NamedEndpoints != nil {
			if loadAssignment.NamedEndpoints == nil {
				loadAssignment.NamedEndpoints = make(map[string]*endpoint.Endpoint)
			}
			for k, v := range edsEndpoints.NamedEndpoints {
				loadAssignment.NamedEndpoints[k] = v
			}
		}

		// Merge policy (last one wins if multiple are set)
		if edsEndpoints.Policy != nil {
			loadAssignment.Policy = edsEndpoints.Policy
		}
	}

	if len(loadAssignment.Endpoints) == 0 && len(loadAssignment.NamedEndpoints) == 0 {
		return nil, referencedEndpoints, 0, nil
	}

	return loadAssignment, referencedEndpoints, totalEndpointCount, nil
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

// removeClusterFromAllNodes removes a cluster from all nodes in the config
func (r *ClusterReconciler) removeClusterFromAllNodes(ctx context.Context, clusterName string) {
	log := ctrllog.FromContext(ctx)
	var removed bool

	for nodeID := range r.Config.ClusterConfigs {
		for i := len(r.Config.ClusterConfigs[nodeID]) - 1; i >= 0; i-- {
			c := r.Config.ClusterConfigs[nodeID][i]
			if c.Name == clusterName {
				r.Config.ClusterConfigs[nodeID] = append(r.Config.ClusterConfigs[nodeID][:i], r.Config.ClusterConfigs[nodeID][i+1:]...)
				removed = true
				r.Config.IncrementConfigCounter()
			}
		}
	}

	if removed {
		log.V(0).Info("Removed cluster from snapshot (waiting for endpoints)", "cluster", clusterName)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Add a Runnable to initialize total count after cache sync
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		log := ctrl.Log.WithName("CDS")

		// Wait for cache to sync
		if !mgr.GetCache().WaitForCacheSync(ctx) {
			return fmt.Errorf("failed to sync cache")
		}

		// Now it's safe to list resources
		var clusterConfigList envoyxdsv1alpha1.ClusterList
		if err := r.List(ctx, &clusterConfigList); err != nil {
			return fmt.Errorf("unable to list ClusterConfigs: %w", err)
		}

		// Initialize reconciliation status
		count := len(clusterConfigList.Items)
		log.Info("Initializing CDS controller", "resources", count)
		if count > 0 {
			r.Config.ReconciliationStatus.SetHasClusters(true)
			log.Info("CDS reconciliation starting", "resources", count)
		} else {
			log.Info("CDS reconciliation complete", "resources", 0)
		}
		// Mark clusters controller as initialized
		r.Config.ReconciliationStatus.SetClustersInitialized(true)
		return nil
	})); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&envoyxdsv1alpha1.Cluster{}).
		Watches(&envoyxdsv1alpha1.Endpoint{}, handler.EnqueueRequestsFromMapFunc(
			func(ctx context.Context, a client.Object) []reconcile.Request {
				ep, ok := a.(*envoyxdsv1alpha1.Endpoint)
				if !ok {
					return nil
				}
				log := ctrllog.FromContext(ctx)

				// Find all clusters that reference this endpoint
				var clusterList envoyxdsv1alpha1.ClusterList
				if err := r.List(context.Background(), &clusterList, &client.ListOptions{
					Namespace: ep.Namespace,
				}); err != nil {
					log.Error(err, "Failed to list clusters")
					return nil
				}

				var requests []reconcile.Request
				for _, cluster := range clusterList.Items {
					for _, ref := range cluster.Spec.EndpointRefs {
						if ref == ep.Name {
							requests = append(requests, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name:      cluster.Name,
									Namespace: cluster.Namespace,
								},
							})
							break
						}
					}
				}

				if len(requests) > 0 {
					log.V(1).Info("Endpoint changed, triggering cluster reconciliation", "endpoint", ep.Name, "clusters", len(requests))
				}

				return requests
			}),
		).
		Complete(r)
}

// ClusterRecast converts CDS types to Envoy Cluster configuration.
func ClusterRecast(c cdstypes.CDS) (*cluster.Cluster, error) {
	clusterConfig := &cluster.Cluster{}

	// Marshal CDS to JSON
	routeData, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("JSON marshaling failed: %w", err)
	}

	// Unmarshal JSON to clusterConfig
	err = protojson.Unmarshal(routeData, clusterConfig)
	if err != nil {
		return nil, fmt.Errorf("proto unmarshaling failed: %w", err)
	}

	return clusterConfig, nil
}

// updateClusterStatus updates the status of the Cluster CR
func (r *ClusterReconciler) updateClusterStatus(ctx context.Context, clusterCR *envoyxdsv1alpha1.Cluster, active bool, activeNodes []string, endpointCount int, referencedEndpoints []string, message string) error {
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
	clusterKey := types.NamespacedName{Name: clusterCR.Name, Namespace: clusterCR.Namespace}
	generation := clusterCR.Generation

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch the latest version of the Cluster to get the current resourceVersion
		var latestCluster envoyxdsv1alpha1.Cluster
		if err := r.Get(ctx, clusterKey, &latestCluster); err != nil {
			return err
		}

		// Build conditions from the latest cluster's conditions
		conditions := latestCluster.Status.Conditions
		if conditions == nil {
			conditions = []metav1.Condition{}
		}

		// Update Ready condition
		readyCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.ClusterConditionReady,
			LastTransitionTime: now,
			ObservedGeneration: generation,
		}
		if active {
			readyCondition.Status = metav1.ConditionTrue
			readyCondition.Reason = "Active"
			if endpointCount > 0 {
				readyCondition.Message = fmt.Sprintf("Cluster is active in %d snapshots with %d endpoints", len(snapshots), endpointCount)
			} else {
				readyCondition.Message = fmt.Sprintf("Cluster is active in %d snapshots", len(snapshots))
			}
		} else {
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = "Inactive"
			readyCondition.Message = message
		}
		conditions = updateCondition(conditions, readyCondition)

		// Update Reconciled condition
		reconciledCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.ClusterConditionReconciled,
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
				Type:               envoyxdsv1alpha1.ClusterConditionError,
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
				Type:               envoyxdsv1alpha1.ClusterConditionError,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: now,
				Reason:             "NoError",
				Message:            "",
				ObservedGeneration: generation,
			}
			conditions = updateCondition(conditions, errorCondition)
		}

		// Prepare the new status
		newStatus := envoyxdsv1alpha1.ClusterStatus{
			Active:              active,
			EndpointCount:       endpointCount,
			ReferencedEndpoints: strings.Join(referencedEndpoints, ","),
			Snapshots:           snapshots,
			Nodes:               strings.Join(nodesList, ","),
			Clusters:            strings.Join(clustersList, ","),
			LastReconciled:      now,
			ObservedGeneration:  generation,
			Conditions:          conditions,
			Message:             message,
		}

		// Update status if changed
		if !clusterStatusEqual(latestCluster.Status, newStatus) {
			latestCluster.Status = newStatus
			if err := r.Status().Update(ctx, &latestCluster); err != nil {
				return err
			}
			log.V(2).Info("Updated cluster status", "active", active, "endpointCount", endpointCount, "nodes", strings.Join(nodesList, ","))
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

// clusterStatusEqual compares two ClusterStatus objects (ignoring LastReconciled time for comparison)
func clusterStatusEqual(a, b envoyxdsv1alpha1.ClusterStatus) bool {
	if a.Active != b.Active {
		return false
	}
	if a.EndpointCount != b.EndpointCount {
		return false
	}
	if a.ReferencedEndpoints != b.ReferencedEndpoints {
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
