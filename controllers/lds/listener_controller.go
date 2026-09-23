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
	"slices"
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
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	"github.com/tentens-tech/xds-controller/pkg/xds/fcm"
	hcmtypes "github.com/tentens-tech/xds-controller/pkg/xds/types/hcm"
	_ "github.com/tentens-tech/xds-controller/pkg/xds/types/route" // imported for RouteSpec embedding
)

// ListenerReconciler reconciles a Listener object
type ListenerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *xds.Config
	// RouteEvents carries listeners RDS changed; when nil, Route objects are watched directly.
	RouteEvents            <-chan event.GenericEvent
	reconciling            atomic.Int32
	lastReconcileTime      atomic.Int64
	initialStartLogged     atomic.Bool
	initialReconcileLogged atomic.Bool
	chains                 chainCache
}

// ErrorDuplicateFound is returned when a route's filter chain would duplicate one already on the listener.
var ErrorDuplicateFound = errors.New("duplicate found")

//+kubebuilder:rbac:groups=envoyxds.io,resources=listeners,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=envoyxds.io,resources=listeners/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=envoyxds.io,resources=listeners/finalizers,verbs=update
//+kubebuilder:rbac:groups=envoyxds.io,resources=routes/status,verbs=get;update;patch

// Reconcile reconciles the Listener resource.
func (r *ListenerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reconcileErr error) {
	log := ctrllog.FromContext(ctx)

	r.Config.ReconciliationStatus.SetHasListeners(true)

	// Wait until RDS has listed and placed routes, so listeners are not built without them.
	rs := r.Config.ReconciliationStatus
	if !rs.IsRoutesInitialized() || !rs.IsRoutesReconciled() || !rs.IsDomainConfigsReconciled() {
		return ctrl.Result{Requeue: true, RequeueAfter: 1 * time.Second}, nil
	}
	// A failed reconcile is retried, so the listener stays pending until a rebuild succeeds.
	pendingKey := req.String()
	pendingSeq := rs.ListenerPendingSeq(pendingKey)
	defer func() {
		if reconcileErr == nil {
			rs.ClearListenerPending(pendingKey, pendingSeq)
		}
	}()

	// Log only once when LDS actually starts reconciling (dependencies ready)
	if !r.initialStartLogged.Swap(true) {
		ctrl.Log.WithName("LDS").Info("LDS reconciliation starting")
	}
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

	defer r.pruneChains()

	// Build the set of previous nodes where the listener exists
	previousNodeSet := make(map[string]struct{})
	r.Config.RLockConfig()
	for nodeID, listenerList := range r.Config.ListenerConfigs {
		for _, l := range listenerList {
			if l.Name == req.Name {
				previousNodeSet[nodeID] = struct{}{}
				break
			}
		}
	}
	r.Config.RUnlockConfig()

	// If listener not found, remove it and mark as processed
	if !listenerFound {
		r.removeListenerFromNodes(ctx, req.Name, previousNodeSet)
		return ctrl.Result{}, nil
	}

	currentNodes := r.getNodesForListener(&listenerCR)
	hasFilterChans := listenerCR.Spec.FilterChains != nil
	hasRoutes := r.hasRoutesFor(req.Name, currentNodes)

	// Track status update info
	var statusErr error
	activeNodes := make([]string, 0, len(currentNodes))
	var filterChainCount int

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
		lds, err := listenerRecast(listenerCR, nodeRoutes, node, &r.chains)
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
		// Mark listeners controller as initialized
		r.Config.ReconciliationStatus.SetListenersInitialized(true)
		return nil
	})); err != nil {
		return err
	}

	b := ctrl.NewControllerManagedBy(mgr).For(&envoyxdsv1alpha1.Listener{})
	if r.RouteEvents != nil {
		b = b.WatchesRawSource(source.Channel(r.RouteEvents, &handler.EnqueueRequestForObject{}))
	} else {
		b = b.Watches(&envoyxdsv1alpha1.Route{}, handler.EnqueueRequestsFromMapFunc(
			func(_ context.Context, a client.Object) []reconcile.Request {
				route, ok := a.(*envoyxdsv1alpha1.Route)
				if !ok || len(route.Spec.ListenerRefs) == 0 {
					return nil
				}
				r.Config.ReconciliationStatus.SetListenersReconciled(false)
				requests := make([]reconcile.Request, 0, len(route.Spec.ListenerRefs))
				for _, ln := range route.Spec.ListenerRefs {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{Name: ln, Namespace: a.GetNamespace()},
					})
				}
				return requests
			}),
		)
	}
	return b.Complete(r)
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
	}
	r.Config.UnlockConfig()

	if removed {
		log.V(0).WithName(listenerName).Info("Removed listener")
	}
}

func (r *ListenerReconciler) getNodesForListener(listenerCR *envoyxdsv1alpha1.Listener) []string {
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

	return util.NodeIDs(listenerCR.Annotations, r.Config.NodeID, r.Config.Cluster)
}

// getRoutesForNode returns routes attached to listener l on the node, oldest first so
// duplicate resolution and filter chain order are stable across restarts.
func (r *ListenerReconciler) getRoutesForNode(l envoyxdsv1alpha1.Listener, nodeid string) []*envoyxdsv1alpha1.Route {
	var nodeRoutes []*envoyxdsv1alpha1.Route
	nodeInfo, _ := util.GetNodeInfo(nodeid) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore
	r.Config.RLockConfig()
	for node, routes := range r.Config.RouteConfigs {
		if !nodeInfo.FindNodeAndCluster(node) {
			continue
		}
		for _, rc := range routes {
			if rc.Route != nil && slices.Contains(rc.Route.Spec.ListenerRefs, l.Name) {
				nodeRoutes = append(nodeRoutes, rc.Route)
			}
		}
	}
	r.Config.RUnlockConfig()

	slices.SortFunc(nodeRoutes, func(a, b *envoyxdsv1alpha1.Route) int {
		switch {
		case util.OlderRoute(a, b):
			return -1
		case util.OlderRoute(b, a):
			return 1
		}
		return 0
	})
	return nodeRoutes
}

// hasRoutesFor reports whether any route on the given nodes attaches to the listener.
func (r *ListenerReconciler) hasRoutesFor(listener string, nodes []string) bool {
	r.Config.RLockConfig()
	defer r.Config.RUnlockConfig()
	for _, node := range nodes {
		nodeInfo, _ := util.GetNodeInfo(node) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore
		for routeNode, routes := range r.Config.RouteConfigs {
			if !nodeInfo.FindNodeAndCluster(routeNode) {
				continue
			}
			for _, rc := range routes {
				if slices.Contains(rc.ListenerNames, listener) {
					return true
				}
			}
		}
	}
	return false
}

func (r *ListenerReconciler) pruneChains() {
	live := make(map[*envoyxdsv1alpha1.Route]struct{})
	r.Config.RLockConfig()
	for _, routes := range r.Config.RouteConfigs {
		for _, rc := range routes {
			live[rc.Route] = struct{}{}
		}
	}
	r.Config.RUnlockConfig()
	r.chains.prune(live)
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

	// Use retry to handle conflicts when updating status
	listenerKey := types.NamespacedName{Name: listenerCR.Name, Namespace: listenerCR.Namespace}
	generation := listenerCR.Generation

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch the latest version of the Listener to get the current resourceVersion
		var latestListener envoyxdsv1alpha1.Listener
		if err := r.Get(ctx, listenerKey, &latestListener); err != nil {
			return err
		}

		// Build conditions from the latest listener's conditions
		conditions := latestListener.Status.Conditions
		if conditions == nil {
			conditions = []metav1.Condition{}
		}

		// Update Ready condition
		readyCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.ListenerConditionReady,
			LastTransitionTime: now,
			ObservedGeneration: generation,
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
			ObservedGeneration: generation,
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
				ObservedGeneration: generation,
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
				ObservedGeneration: generation,
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
			ObservedGeneration: generation,
			Conditions:         conditions,
			Message:            message,
		}

		// Update status if changed
		if !listenerStatusEqual(latestListener.Status, newStatus) {
			latestListener.Status = newStatus
			if err := r.Status().Update(ctx, &latestListener); err != nil {
				return err
			}
			log.V(1).Info("Updated listener status", "active", active, "filterChainCount", filterChainCount, "nodes", strings.Join(nodesList, ","))
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
	return listenerRecast(l, routes, node, nil)
}

func listenerRecast(l envoyxdsv1alpha1.Listener, routes []*envoyxdsv1alpha1.Route, node string, chains *chainCache) (*listenerv3.Listener, error) {
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

	lds, err = processRoutes(routes, lds, node, chains)
	if err != nil {
		return nil, fmt.Errorf("unable to process routes: %w", err)
	}

	return lds, nil
}

func configSource() *corev3.ConfigSource {
	cs := &corev3.ConfigSource{}
	cs.ResourceApiVersion = resource.DefaultAPIVersion
	cs.ConfigSourceSpecifier = &corev3.ConfigSource_Ads{
		Ads: &corev3.AggregatedConfigSource{},
	}

	return cs
}

func isQUIC(l *listenerv3.Listener) bool {
	return l.UdpListenerConfig != nil &&
		l.UdpListenerConfig.QuicOptions != nil &&
		l.GetAddress().GetSocketAddress().GetProtocol() == corev3.SocketAddress_UDP
}

func prepareFilters(route *envoyxdsv1alpha1.Route, quic bool) ([]*listenerv3.Filter, error) {
	// Copies keep the stored Route untouched; the outer route_config shadowed HCM's own.
	h := route.Spec.HCM
	h.RouteConfig = nil
	rdsCfg := hcmtypes.Rds{}
	if h.Rds != nil {
		rdsCfg = *h.Rds
	}
	rdsCfg.RouteConfigName = route.Name
	cs := hcmtypes.ConfigSource{}
	if rdsCfg.ConfigSource != nil {
		cs = *rdsCfg.ConfigSource
	}
	// ADS config must have content (even empty object) to not be omitted by omitempty
	cs.Ads = &runtime.RawExtension{Raw: []byte("{}")}
	cs.ResourceApiVersion = "V3"
	rdsCfg.ConfigSource = &cs
	h.Rds = &rdsCfg

	routeData, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("json marshaling error: %w", err)
	}

	hManager := &hcm.HttpConnectionManager{}
	if err = protojson.Unmarshal(routeData, hManager); err != nil {
		return nil, fmt.Errorf("proto unmarshalling error: %w", err)
	}

	if quic && hManager.CodecType == hcm.HttpConnectionManager_AUTO {
		hManager.CodecType = hcm.HttpConnectionManager_HTTP3
	}

	tConfig, err := anypb.New(hManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create typed config: %w", err)
	}
	return []*listenerv3.Filter{{
		Name: wellknown.HTTPConnectionManager,
		ConfigType: &listenerv3.Filter_TypedConfig{
			TypedConfig: tConfig,
		},
	}}, nil
}

func processRoutes(routes []*envoyxdsv1alpha1.Route, lds *listenerv3.Listener, node string, chains *chainCache) (*listenerv3.Listener, error) {
	var placed fcm.Set
	for _, fc := range lds.FilterChains {
		m, err := fcm.CompileProto(fc.GetFilterChainMatch())
		if err != nil {
			return nil, fmt.Errorf("filter chain %q: %w", fc.GetName(), err)
		}
		if placed.HasDuplicate(m) {
			return nil, fmt.Errorf("filter chain %q: Envoy cannot tell it apart from another chain in spec.filter_chains", fc.GetName())
		}
		placed.Add(m)
	}
	quic := isQUIC(lds)
	for _, route := range routes {
		m, err := processRoute(chains.get(route, quic), route, lds, node, &placed)
		if errors.Is(err, ErrorDuplicateFound) {
			continue
		}
		if err != nil {
			// One bad route must not stop the rest of the listener from updating.
			ctrllog.FromContext(context.Background()).Error(err, "route skipped", "route", route.Name, "listener", lds.GetName())
			xds.RecordConfigError(route.Name, "LDS", err.Error())
			continue
		}
		placed.Add(m)
	}
	return lds, nil
}

func processRoute(c *builtChain, route *envoyxdsv1alpha1.Route, lds *listenerv3.Listener, node string, placed *fcm.Set) (*fcm.Matcher, error) {
	if c.matchErr != nil {
		return nil, c.matchErr
	}
	if duplicateFound(route, c.matcher, placed, node) {
		return nil, ErrorDuplicateFound
	}
	if c.chainErr != nil {
		return nil, c.chainErr
	}
	lds.FilterChains = append(lds.FilterChains, c.chain)
	return c.matcher, nil
}

// builtChain is the filter chain for a route; it depends only on the route and on whether the listener is QUIC.
type builtChain struct {
	matcher  *fcm.Matcher
	matchErr error
	chain    *listenerv3.FilterChain
	chainErr error
}

func buildChain(route *envoyxdsv1alpha1.Route, quic bool) *builtChain {
	c := &builtChain{}
	var routeFCM *listenerv3.FilterChainMatch
	if route.Spec.FilterChainMatch != nil {
		if routeFCM, c.matchErr = unmarshalFilterChainMatch(route); c.matchErr != nil {
			return c
		}
	}
	if c.matcher, c.matchErr = fcm.CompileProto(routeFCM); c.matchErr != nil {
		c.matchErr = fmt.Errorf("route %q: %w", route.Name, c.matchErr)
		return c
	}
	filters, err := prepareFilters(route, quic)
	if err != nil {
		c.chainErr = fmt.Errorf("unable to prepare filter: %w", err)
		return c
	}
	c.chain = prepareSecretContext(route, quic, &listenerv3.FilterChain{
		FilterChainMatch: routeFCM,
		Filters:          filters,
	})
	return c
}

// chainCache shares built chains across listener rebuilds and nodes. RDS stores a new Route
// pointer for every change, so the pointer is the key.
type chainCache struct {
	mu      sync.Mutex
	entries map[chainKey]*builtChain
}

type chainKey struct {
	route *envoyxdsv1alpha1.Route
	quic  bool
}

func (c *chainCache) get(route *envoyxdsv1alpha1.Route, quic bool) *builtChain {
	if c == nil {
		return buildChain(route, quic)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := chainKey{route, quic}
	if b, ok := c.entries[k]; ok {
		return b
	}
	if c.entries == nil {
		c.entries = make(map[chainKey]*builtChain)
	}
	b := buildChain(route, quic)
	c.entries[k] = b
	return b
}

// prune drops chains of routes RDS no longer stores.
func (c *chainCache) prune(live map[*envoyxdsv1alpha1.Route]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if _, ok := live[k.route]; !ok {
			delete(c.entries, k)
		}
	}
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

// duplicateFound reports whether Envoy would reject the listener for holding both
// the route's chain and one already placed; the older placement wins.
func duplicateFound(route *envoyxdsv1alpha1.Route, m *fcm.Matcher, placed *fcm.Set, node string) bool {
	if !placed.HasDuplicate(m) {
		return false
	}
	const msg = "filter chain duplicates one already on the listener, route skipped"
	nodeInfo, _ := util.GetNodeInfo(node) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore
	ctrllog.FromContext(context.Background()).Info(msg, "route", route.Name, "node", nodeInfo.Nodes, "cluster", nodeInfo.Clusters)
	xds.RecordConfigError(route.Name, "LDS", msg)
	return true
}

// routeTLSSecretNames returns TLSSecret CR names for the route filter chain transport socket.
// tlssecret_ref (if set) is listed first, then tlssecret_refs, with duplicates removed.
func routeTLSSecretNames(route *envoyxdsv1alpha1.Route) []string {
	if route == nil {
		return nil
	}
	var names []string
	seen := make(map[string]struct{})
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		names = append(names, s)
	}
	add(route.Spec.TLSSecretRef)
	for _, ref := range route.Spec.TLSSecretRefs {
		add(ref)
	}
	return names
}

func prepareSecretContext(route *envoyxdsv1alpha1.Route, quic bool, filter *listenerv3.FilterChain) *listenerv3.FilterChain {
	secretNames := routeTLSSecretNames(route)
	if len(secretNames) == 0 {
		return filter
	}

	sdsConfigs := make([]*authv3.SdsSecretConfig, 0, len(secretNames))
	for _, name := range secretNames {
		sdsConfigs = append(sdsConfigs, &authv3.SdsSecretConfig{
			Name:      name,
			SdsConfig: configSource(),
		})
	}

	if quic {
		// Setup QUIC transport socket
		quicTransport := &quicv3.QuicDownstreamTransport{
			DownstreamTlsContext: &authv3.DownstreamTlsContext{
				CommonTlsContext: &authv3.CommonTlsContext{
					TlsCertificateSdsSecretConfigs: sdsConfigs,
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
				TlsCertificateSdsSecretConfigs: sdsConfigs,
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
