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

// Package lds implements the LDS (Listener Discovery Service) controller.
package lds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3" // registers trace types

	// Access loggers
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/grpc/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/open_telemetry/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/stream/v3"

	// Compression extensions
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/brotli/compressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/brotli/decompressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/gzip/compressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/gzip/decompressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/zstd/compressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/zstd/decompressor/v3"

	// HTTP filters
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/buffer/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/compressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_stats/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_web/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/header_to_metadata/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/health_check/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/local_ratelimit/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/oauth2/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ratelimit/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"

	// Listener filters
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/http_inspector/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_dst/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/proxy_protocol/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"

	// Network filters - blank imports register protobuf types for Envoy xDS config unmarshalling
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/ext_authz/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/mongo_proxy/v3" // protobuf type registration
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/ratelimit/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/redis_proxy/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"

	// Retry host predicates
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/omit_canary_hosts/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/omit_host_metadata/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/previous_hosts/v3"

	// Tracers
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/resource_detectors/v3"

	// Transport sockets - blank imports register protobuf types for Envoy xDS config unmarshalling
	quicv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/quic/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3" // protobuf type registration
	authv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	// Upstreams
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	hcmtypes "github.com/tentens-tech/xds-controller/pkg/xds/types/hcm"
	_ "github.com/tentens-tech/xds-controller/pkg/xds/types/route" // imported for RouteSpec embedding
)

// ListenerReconciler reconciles a Listener object
type ListenerReconciler struct {
	client.Client
	Scheme                 *runtime.Scheme
	Config                 *xds.Config
	reconciling            atomic.Int32
	lastReconcileTime      atomic.Int64
	initialStartLogged     atomic.Bool
	initialReconcileLogged atomic.Bool
}

// ErrorDuplicateFound is returned when a duplicate domain is found in filter chains.
var ErrorDuplicateFound = errors.New("duplicate found")

//+kubebuilder:rbac:groups=envoyxds.io,resources=listeners,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=envoyxds.io,resources=listeners/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=envoyxds.io,resources=listeners/finalizers,verbs=update
//+kubebuilder:rbac:groups=envoyxds.io,resources=routes/status,verbs=get;update;patch

// Reconcile reconciles the Listener resource.
func (r *ListenerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	time.Sleep(1 * time.Second)

	// wait for routes and domain configs to be reconciled - requeue only if both are not ready
	if !r.Config.ReconciliationStatus.IsRoutesReconciled() || !r.Config.ReconciliationStatus.IsDomainConfigsReconciled() {
		r.Config.ReconciliationStatus.SetListenersReconciled(false)
		return ctrl.Result{Requeue: true, RequeueAfter: 1 * time.Second}, nil
	}

	// Log only once when LDS actually starts reconciling (dependencies ready)
	if !r.initialStartLogged.Swap(true) {
		ctrl.Log.WithName("LDS").Info("LDS reconciliation starting")
	}

	r.Config.ReconciliationStatus.SetListenersReconciled(false)
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
				r.Config.ReconciliationStatus.SetListenersReconciled(true)
				// Log only once when initial reconciliation completes
				if !r.initialReconcileLogged.Swap(true) {
					ctrl.Log.WithName("LDS").Info("LDS reconciliation complete")
				}
			}
		}()

		<-reconcileCtx.Done()
		if reconcileCtx.Err() == context.DeadlineExceeded {
			log.Info("Reconciliation timed out")
		}
	}()

	// Fetch the Listener instance
	var listenerCR envoyxdsv1alpha1.Listener
	listenerFound := true
	if err := r.Get(ctx, req.NamespacedName, &listenerCR); err != nil {
		if apierrors.IsNotFound(err) {
			listenerFound = false
		} else {
			log.Error(err, "unable to fetch Listener")
			return ctrl.Result{}, err
		}
	}

	// Build the set of previous nodes where the listener exists
	previousNodeSet := make(map[string]struct{})
	for nodeID, listenerList := range r.Config.ListenerConfigs {
		for _, l := range listenerList {
			if l.Name == req.Name {
				previousNodeSet[nodeID] = struct{}{}
				break
			}
		}
	}

	// If listener not found, remove it and mark as processed
	if !listenerFound {
		r.removeListenerFromNodes(ctx, req.Name, previousNodeSet)
		return ctrl.Result{}, nil
	}

	// Track status update info
	var statusErr error
	activeNodes := make([]string, 0, len(r.Config.ListenerConfigs))
	var filterChainCount int

	// Process the Listener for each node
	currentNodes := r.getNodesForListener(&listenerCR)
	hasFilterChans := listenerCR.Spec.FilterChains != nil

	hasRoutes := false
	for _, node := range currentNodes {
		nodeInfo, _ := util.GetNodeInfo(node) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore
		for routeNode := range r.Config.RouteConfigs {
			if nodeInfo.FindNodeAndCluster(routeNode) {
				for _, route := range r.Config.RouteConfigs[routeNode] {
					for _, listenerName := range route.ListenerNames {
						if listenerName == req.Name {
							hasRoutes = true
							break
						}
					}
					if hasRoutes {
						break
					}
				}
			}
		}
		if hasRoutes {
			break
		}
	}

	// If no routes found, remove listener and mark as processed
	if !hasFilterChans && !hasRoutes {
		r.removeListenerFromNodes(ctx, req.Name, previousNodeSet)
		// Update status to inactive
		if err := r.updateListenerStatus(ctx, &listenerCR, false, nil, 0, "No routes or filter chains configured"); err != nil {
			log.Error(err, "unable to update Listener status")
		}
		return ctrl.Result{}, nil
	}

	// Build the set of current nodes
	currentNodeSet := make(map[string]struct{})
	for _, node := range currentNodes {
		currentNodeSet[node] = struct{}{}
	}

	// Remove listener from nodes where it no longer belongs
	nodesToRemove := make(map[string]struct{})
	for nodeID := range previousNodeSet {
		if _, exists := currentNodeSet[nodeID]; !exists {
			nodesToRemove[nodeID] = struct{}{}
		}
	}
	r.removeListenerFromNodes(ctx, req.Name, nodesToRemove)

	// Update or add listener to the current nodes
	for _, node := range currentNodes {
		// Get the routes applicable for this node
		nodeRoutes := r.getRoutesForNode(listenerCR, node)

		if !hasFilterChans && len(nodeRoutes) == 0 {
			r.removeListenerFromNodes(ctx, req.Name, map[string]struct{}{node: {}})
			continue
		}

		// Recast the Listener
		lds, err := ListenerRecast(listenerCR, nodeRoutes, node)
		if err != nil {
			log.Error(err, "unable to recast Listener")
			statusErr = err
			continue
		}

		// Update the ListenerConfigs
		r.updateListenerConfig(ctx, node, lds)

		// Track active nodes and filter chain count
		activeNodes = append(activeNodes, node)
		if len(lds.FilterChains) > filterChainCount {
			filterChainCount = len(lds.FilterChains)
		}
	}

	// Update the status
	statusMessage := ""
	if statusErr != nil {
		statusMessage = statusErr.Error()
	}
	if err := r.updateListenerStatus(ctx, &listenerCR, len(activeNodes) > 0, activeNodes, filterChainCount, statusMessage); err != nil {
		log.Error(err, "unable to update Listener status")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ListenerReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Add a Runnable to initialize total count after cache sync
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		log := ctrl.Log.WithName("LDS")

		// Wait for cache to sync
		if !mgr.GetCache().WaitForCacheSync(ctx) {
			return fmt.Errorf("failed to sync cache")
		}

		// Now it's safe to list resources
		var listenerConfigList envoyxdsv1alpha1.ListenerList
		if err := r.List(ctx, &listenerConfigList); err != nil {
			return fmt.Errorf("unable to list Listeners: %w", err)
		}

		// Initialize reconciliation status
		count := len(listenerConfigList.Items)
		log.Info("Initializing LDS controller", "resources", count)
		if count > 0 {
			r.Config.ReconciliationStatus.SetHasListeners(true)
			log.Info("LDS waiting for RDS and SDS to complete", "resources", count)
		} else {
			log.Info("LDS reconciliation complete", "resources", 0)
		}
		return nil
	})); err != nil {
		return err
	}

	// Create a debounce map to track the last update time for each listener
	debounceMap := make(map[string]time.Time)
	debounceMapMutex := &sync.Mutex{}
	debounceInterval := 1 * time.Second // Adjust this value based on your needs

	// Map to track the last observed generation of routes
	routeGenerations := make(map[string]int64)
	routeGenerationsMutex := &sync.Mutex{}

	return ctrl.NewControllerManagedBy(mgr).
		For(&envoyxdsv1alpha1.Listener{}).
		Watches(&envoyxdsv1alpha1.Route{}, handler.EnqueueRequestsFromMapFunc(
			func(_ context.Context, a client.Object) []reconcile.Request {
				route, ok := a.(*envoyxdsv1alpha1.Route)
				if !ok || len(route.Spec.ListenerRefs) == 0 {
					return nil
				}

				r.Config.ReconciliationStatus.SetListenersReconciled(false)

				// Collect all requests for all listeners
				var requests []reconcile.Request

				for _, ln := range route.Spec.ListenerRefs {
					// Get the key for the listener
					key := fmt.Sprintf("%s/%s", a.GetNamespace(), ln)

					// Get nodeID from route annotations
					nodeID := util.GetNodeID(a.GetAnnotations())
					routeKey := fmt.Sprintf("%s/%s#%s", a.GetNamespace(), a.GetName(), nodeID)

					// Get the current generation of the route
					currentGeneration := a.GetGeneration()

					// Check if this is a new generation of the route
					routeGenerationsMutex.Lock()
					lastGeneration, exists := routeGenerations[routeKey]
					isNewGeneration := !exists || lastGeneration != currentGeneration
					if isNewGeneration {
						routeGenerations[routeKey] = currentGeneration
					}
					routeGenerationsMutex.Unlock()

					// Check debounce timing
					debounceMapMutex.Lock()
					lastUpdate, exists := debounceMap[key]
					now := time.Now()
					shouldReconcile := !exists || now.Sub(lastUpdate) > debounceInterval

					if shouldReconcile {
						debounceMap[key] = now
						requests = append(requests, reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name:      ln,
								Namespace: a.GetNamespace(),
							},
						})
					}
					debounceMapMutex.Unlock()
				}

				return requests
			}),
		).
		Complete(r)
}

func (r *ListenerReconciler) removeListenerFromNodes(ctx context.Context, listenerName string, nodes map[string]struct{}) {
	log := ctrllog.FromContext(ctx)
	var removed bool

	r.Config.LockConfig()
	for nodeID := range nodes {
		for i := len(r.Config.ListenerConfigs[nodeID]) - 1; i >= 0; i-- {
			l := r.Config.ListenerConfigs[nodeID][i]
			if l.Name == listenerName {
				r.Config.ListenerConfigs[nodeID] = append(r.Config.ListenerConfigs[nodeID][:i], r.Config.ListenerConfigs[nodeID][i+1:]...)
				removed = true
				r.Config.IncrementConfigCounter()
			}
		}

		// Remove associated routes
		for i := len(r.Config.RouteConfigs[nodeID]) - 1; i >= 0; i-- {
			route := r.Config.RouteConfigs[nodeID][i]
			if route.Route == nil {
				continue
			}

			// Check if this route is associated with the listener being removed
			if len(route.Route.Spec.ListenerRefs) > 0 {
				for _, listenerRef := range route.Route.Spec.ListenerRefs {
					if listenerRef == listenerName {
						log.V(0).WithName(listenerName).Info("Removing associated route config", "route", route.Route.Name)
						r.Config.RouteConfigs[nodeID] = append(r.Config.RouteConfigs[nodeID][:i], r.Config.RouteConfigs[nodeID][i+1:]...)
						r.Config.IncrementConfigCounter()
						break
					}
				}
			}
		}
	}
	r.Config.UnlockConfig()

	if removed {
		log.V(0).WithName(listenerName).Info("Removed listener")
	}
}

func (r *ListenerReconciler) getNodesForListener(listenerCR *envoyxdsv1alpha1.Listener) []string {
	nodes := []string{}
	// Set default nodes and clusters if not present
	if listenerCR.Annotations == nil {
		listenerCR.Annotations = make(map[string]string)
	}
	if listenerCR.Annotations["nodes"] == "" {
		listenerCR.Annotations["nodes"] = r.Config.NodeID
	}
	if listenerCR.Annotations["clusters"] == "" {
		listenerCR.Annotations["clusters"] = r.Config.Cluster
	}

	// Parse nodes and clusters from annotations
	nodesList := util.ParseCSV(listenerCR.Annotations["nodes"])
	clustersList := util.ParseCSV(listenerCR.Annotations["clusters"])

	// Sort nodes and clusters for consistent NodeID generation
	sort.Strings(nodesList)
	sort.Strings(clustersList)

	for _, cluster := range clustersList {
		for _, node := range nodesList {
			nodeID := util.GetNodeID(map[string]string{"clusters": cluster, "nodes": node})
			nodes = append(nodes, nodeID)
		}
	}
	return nodes
}

func (r *ListenerReconciler) getRoutesForNode(l envoyxdsv1alpha1.Listener, nodeid string) []*envoyxdsv1alpha1.Route {
	var nodeRoutes []*envoyxdsv1alpha1.Route
	nodeInfo, _ := util.GetNodeInfo(nodeid) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore
	for node := range r.Config.RouteConfigs {
		if nodeInfo.FindNodeAndCluster(node) {
			for _, route := range r.Config.RouteConfigs[node] {
				// Get listener_refs from spec and check if current listener is included
				if route.Route != nil && len(route.Route.Spec.ListenerRefs) > 0 {
					for _, ln := range route.Route.Spec.ListenerRefs {
						if ln == l.Name {
							nodeRoutes = append(nodeRoutes, route.Route)
							break
						}
					}
				}
			}
		}
	}

	return nodeRoutes
}

func (r *ListenerReconciler) updateListenerConfig(ctx context.Context, node string, lds *listenerv3.Listener) {
	log := ctrllog.FromContext(ctx)
	nodeInfo, _ := util.GetNodeInfo(node) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore

	r.Config.LockConfig()
	defer r.Config.UnlockConfig()

	if r.Config.ListenerConfigs == nil {
		r.Config.ListenerConfigs = make(map[string][]*listenerv3.Listener)
	}

	var updated bool
	for i, l := range r.Config.ListenerConfigs[node] {
		if l.Name != lds.Name {
			continue
		}
		r.Config.ListenerConfigs[node][i] = lds
		updated = true
		log.V(0).Info("Updated listener", "filtersCount", len(lds.FilterChains), "nodes", nodeInfo.Nodes, "clusters", nodeInfo.Clusters)
		r.Config.IncrementConfigCounter()
		break
	}

	if !updated {
		r.Config.ListenerConfigs[node] = append(r.Config.ListenerConfigs[node], lds)
		log.V(2).Info("Added listener", "filtersCount", len(lds.FilterChains), "nodes", nodeInfo.Nodes, "clusters", nodeInfo.Clusters)
		r.Config.IncrementConfigCounter()
	}
}

// updateListenerStatus updates the status of the Listener CR
func (r *ListenerReconciler) updateListenerStatus(ctx context.Context, listenerCR *envoyxdsv1alpha1.Listener, active bool, activeNodes []string, filterChainCount int, message string) error {
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

	// Build conditions
	conditions := listenerCR.Status.Conditions
	if conditions == nil {
		conditions = []metav1.Condition{}
	}

	// Update Ready condition
	readyCondition := metav1.Condition{
		Type:               envoyxdsv1alpha1.ListenerConditionReady,
		LastTransitionTime: now,
		ObservedGeneration: listenerCR.Generation,
	}
	if active {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "Active"
		readyCondition.Message = fmt.Sprintf("Listener is active in %d snapshots", len(snapshots))
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "Inactive"
		readyCondition.Message = message
	}
	conditions = updateCondition(conditions, readyCondition)

	// Update Reconciled condition
	reconciledCondition := metav1.Condition{
		Type:               envoyxdsv1alpha1.ListenerConditionReconciled,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: now,
		Reason:             "Reconciled",
		Message:            "Successfully reconciled",
		ObservedGeneration: listenerCR.Generation,
	}
	conditions = updateCondition(conditions, reconciledCondition)

	// Update Error condition if there's an error
	if message != "" && !active {
		errorCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.ListenerConditionError,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             "Error",
			Message:            message,
			ObservedGeneration: listenerCR.Generation,
		}
		conditions = updateCondition(conditions, errorCondition)
	} else {
		// Clear error condition
		errorCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.ListenerConditionError,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             "NoError",
			Message:            "",
			ObservedGeneration: listenerCR.Generation,
		}
		conditions = updateCondition(conditions, errorCondition)
	}

	// Prepare the new status
	newStatus := envoyxdsv1alpha1.ListenerStatus{
		Active:             active,
		FilterChainCount:   filterChainCount,
		Snapshots:          snapshots,
		Nodes:              strings.Join(nodesList, ","),
		Clusters:           strings.Join(clustersList, ","),
		LastReconciled:     now,
		ObservedGeneration: listenerCR.Generation,
		Conditions:         conditions,
		Message:            message,
	}

	// Update status if changed
	if !listenerStatusEqual(listenerCR.Status, newStatus) {
		listenerCR.Status = newStatus
		if err := r.Status().Update(ctx, listenerCR); err != nil {
			log.Error(err, "unable to update Listener status")
			return err
		}
		log.V(1).Info("Updated listener status", "active", active, "filterChainCount", filterChainCount, "nodes", strings.Join(nodesList, ","))
	}

	return nil
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

// listenerStatusEqual compares two ListenerStatus objects (ignoring LastReconciled time for comparison)
func listenerStatusEqual(a, b envoyxdsv1alpha1.ListenerStatus) bool {
	if a.Active != b.Active {
		return false
	}
	if a.FilterChainCount != b.FilterChainCount {
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

// ListenerRecast converts Listener CR to Envoy Listener configuration.
func ListenerRecast(l envoyxdsv1alpha1.Listener, routes []*envoyxdsv1alpha1.Route, node string) (*listenerv3.Listener, error) {
	lds := &listenerv3.Listener{}

	ldsData, err := json.Marshal(l.Spec.LDS)
	if err != nil {
		return nil, fmt.Errorf("json error: %w", err)
	}
	// Unmarshal the listener spec into an Envoy listener
	err = protojson.Unmarshal(ldsData, lds)
	if err != nil {
		return nil, fmt.Errorf("proto error: %w", err)
	}

	lds.Name = l.Name

	// If there are no routes and no filters in the listener's FilterChain, return an error to indicate removal
	if len(routes) == 0 && len(lds.FilterChains) == 0 {
		return nil, fmt.Errorf("no routes or filters in listener")
	}

	lds, err = processRoutes(routes, lds, node)
	if err != nil {
		return nil, fmt.Errorf("unable to process routes: %w", err)
	}

	return lds, nil
}

func configSource() *corev3.ConfigSource {
	source := &corev3.ConfigSource{}
	source.ResourceApiVersion = resource.DefaultAPIVersion
	source.ConfigSourceSpecifier = &corev3.ConfigSource_Ads{
		Ads: &corev3.AggregatedConfigSource{},
	}

	return source
}

func hasFilterChainDuplicates(domains, searchDomains []string) bool {
	for _, domain := range domains {
		for _, searchDomain := range searchDomains {
			if domain == searchDomain {
				return true
			}
		}
	}
	return false
}

func prepareFilters(route envoyxdsv1alpha1.Route, lds *listenerv3.Listener) ([]*listenerv3.Filter, error) {
	filters := []*listenerv3.Filter{}
	// Convert route_config to RDS using the generated HCM types
	if route.Spec.Rds == nil {
		route.Spec.Rds = &hcmtypes.Rds{}
	}
	route.Spec.Rds.RouteConfigName = route.Name
	if route.Spec.Rds.ConfigSource == nil {
		route.Spec.Rds.ConfigSource = &hcmtypes.ConfigSource{}
	}
	// ADS config must have content (even empty object) to not be omitted by omitempty
	route.Spec.Rds.ConfigSource.Ads = &runtime.RawExtension{Raw: []byte("{}")}
	route.Spec.Rds.ConfigSource.ResourceApiVersion = "V3"

	// Ignore filter chain match (it's used separately for the filter chain)
	route.Spec.FilterChainMatch = nil

	// Marshal the route spec (HCM fields + RDS config)
	routeData, err := json.Marshal(route.Spec.Route)
	if err != nil {
		return nil, fmt.Errorf("json marshaling error: %w", err)
	}

	// Unmarshal the JSON into a map to remove fields not needed for HCM
	var data map[string]interface{}
	err = json.Unmarshal(routeData, &data)
	if err != nil {
		return nil, fmt.Errorf("json unmarshalling error: %w", err)
	}

	// Delete fields that are not part of HCM config
	delete(data, "route_config")       // RouteConfiguration is sent via RDS
	delete(data, "filter_chain_match") // Used for filter chain matching
	delete(data, "listener_refs")      // Custom field for route-to-listener binding
	delete(data, "tlssecret_ref")      // Custom field for TLS secret binding

	// Re-marshal the map into JSON
	routeData, err = json.Marshal(data)
	if err != nil {
		return nil, err
	}

	hManager := &hcm.HttpConnectionManager{}

	// Use protojson to unmarshal HttpConnectionManager
	err = protojson.Unmarshal(routeData, hManager)
	if err != nil {
		return nil, fmt.Errorf("proto unmarshalling error: %w", err)
	}

	isQuic := lds.UdpListenerConfig != nil &&
		lds.UdpListenerConfig.QuicOptions != nil &&
		lds.GetAddress().GetSocketAddress().GetProtocol() == corev3.SocketAddress_UDP

	if isQuic && hManager.CodecType == hcm.HttpConnectionManager_AUTO {
		hManager.CodecType = hcm.HttpConnectionManager_HTTP3
	}

	tConfig, err := anypb.New(hManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create typed config: %w", err)
	}
	filters = append(filters, &listenerv3.Filter{
		Name: wellknown.HTTPConnectionManager,
		ConfigType: &listenerv3.Filter_TypedConfig{
			TypedConfig: tConfig,
		},
	})

	return filters, nil
}

func processRoutes(routes []*envoyxdsv1alpha1.Route, lds *listenerv3.Listener, node string) (*listenerv3.Listener, error) {
	for _, route := range routes {
		if err := processRoute(route, lds, node); err != nil {
			if errors.Is(err, ErrorDuplicateFound) {
				continue
			}
			return nil, err
		}
	}
	return lds, nil
}

func processRoute(route *envoyxdsv1alpha1.Route, lds *listenerv3.Listener, node string) error {
	var routeFCM *listenerv3.FilterChainMatch
	var err error

	if route.Spec.FilterChainMatch != nil {
		routeFCM, err = unmarshalFilterChainMatch(route)
		if err != nil {
			return err
		}
	}

	if duplicateFound(lds, route, routeFCM, node) {
		return ErrorDuplicateFound
	}

	filters, err := prepareFilters(*route, lds)
	if err != nil {
		return fmt.Errorf("unable to prepare filter: %w", err)
	}

	filterChain := &listenerv3.FilterChain{
		FilterChainMatch: routeFCM,
		Filters:          filters,
	}

	filterChain = prepareSecretContext(route, lds, filterChain)

	lds.FilterChains = append(lds.FilterChains, filterChain)

	return nil
}

func unmarshalFilterChainMatch(route *envoyxdsv1alpha1.Route) (*listenerv3.FilterChainMatch, error) {
	routeFCM := &listenerv3.FilterChainMatch{}
	fcmData, err := json.Marshal(route.Spec.FilterChainMatch)
	if err != nil {
		return nil, fmt.Errorf("json marshaling error: %w", err)
	}
	if err := protojson.Unmarshal(fcmData, routeFCM); err != nil {
		return nil, fmt.Errorf("proto unmarshalling error: %w", err)
	}
	return routeFCM, nil
}

func duplicateFound(lds *listenerv3.Listener, route *envoyxdsv1alpha1.Route, routeFCM *listenerv3.FilterChainMatch, node string) bool {
	log := ctrllog.FromContext(context.Background())
	for _, filter := range lds.FilterChains {
		if filter != nil && filter.FilterChainMatch != nil && hasFilterChainDuplicates(filter.FilterChainMatch.GetServerNames(), routeFCM.GetServerNames()) {
			nodeInfo, _ := util.GetNodeInfo(node) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore
			log.V(1).Info("unable to add route to listener, duplicate found, skipping", "route", route.Name, "domains", routeFCM.GetServerNames(), "node", nodeInfo.Nodes, "cluster", nodeInfo.Clusters)
			return true
		}
	}
	return false
}

func prepareSecretContext(route *envoyxdsv1alpha1.Route, lds *listenerv3.Listener, filter *listenerv3.FilterChain) *listenerv3.FilterChain {
	if route.Spec.TLSSecretRef == "" {
		return filter
	}

	isQuic := lds.UdpListenerConfig != nil &&
		lds.UdpListenerConfig.QuicOptions != nil &&
		lds.GetAddress().GetSocketAddress().GetProtocol() == corev3.SocketAddress_UDP

	if isQuic {
		// Setup QUIC transport socket
		quicTransport := &quicv3.QuicDownstreamTransport{
			DownstreamTlsContext: &authv3.DownstreamTlsContext{
				CommonTlsContext: &authv3.CommonTlsContext{
					TlsCertificateSdsSecretConfigs: []*authv3.SdsSecretConfig{{
						Name:      route.Spec.TLSSecretRef,
						SdsConfig: configSource(),
					}},
				},
			},
		}

		mt, err := anypb.New(quicTransport)
		if err == nil {
			filter.TransportSocket = &corev3.TransportSocket{
				Name: "envoy.transport_sockets.quic",
				ConfigType: &corev3.TransportSocket_TypedConfig{
					TypedConfig: mt,
				},
			}
		}
	} else {
		// Setup regular TLS transport socket
		tlsc := &authv3.DownstreamTlsContext{
			CommonTlsContext: &authv3.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*authv3.SdsSecretConfig{{
					Name:      route.Spec.TLSSecretRef,
					SdsConfig: configSource(),
				}},
			},
		}
		if filter.FilterChainMatch != nil && filter.FilterChainMatch.ApplicationProtocols != nil {
			tlsc.CommonTlsContext.AlpnProtocols = filter.FilterChainMatch.ApplicationProtocols
		}
		mt, err := anypb.New(tlsc)
		if err == nil {
			filter.TransportSocket = &corev3.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &corev3.TransportSocket_TypedConfig{
					TypedConfig: mt,
				},
			}
		}
	}
	return filter
}
