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

// Package rds implements the RDS (Route Discovery Service) controller.
package rds

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
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

	// Listener filters (for filter chain config references)
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/http_inspector/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_dst/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/proxy_protocol/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"

	// Network filters (for HCM typed configs)
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/ext_authz/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/ratelimit/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"

	// Retry host predicates
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/omit_canary_hosts/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/omit_host_metadata/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/previous_hosts/v3"

	// Tracers
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/resource_detectors/v3"

	// Transport sockets
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	// Upstreams
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	"google.golang.org/protobuf/encoding/protojson"
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
	rdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/rds"
	routetypes "github.com/tentens-tech/xds-controller/pkg/xds/types/route"
)

// RouteReconciler reconciles a Route object
type RouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *xds.Config
	// ListenerEvents receives listeners whose filter chains changed; nil disables notification.
	ListenerEvents         chan<- event.GenericEvent
	requeue                chan event.GenericEvent
	losersMu               sync.Mutex
	losers                 map[types.NamespacedName]map[types.NamespacedName]struct{}
	reconciling            atomic.Int32
	lastReconcileTime      atomic.Int64
	initialReconcileLogged atomic.Bool
}

//+kubebuilder:rbac:groups=envoyxds.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=envoyxds.io,resources=routes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=envoyxds.io,resources=routes/finalizers,verbs=update

// Reconcile places the Route on its nodes, resolving filter chain conflicts by age.
func (r *RouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	// Mark that we have routes to reconcile (handles dynamically added resources)
	r.Config.ReconciliationStatus.SetHasRoutes(true)
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
				r.Config.ReconciliationStatus.SetRoutesReconciled(true)
				// Log only once when initial reconciliation completes
				if !r.initialReconcileLogged.Swap(true) {
					ctrl.Log.WithName("RDS").Info("RDS reconciliation complete")
				}
			}
		}()

		<-reconcileCtx.Done()
		if reconcileCtx.Err() == context.DeadlineExceeded {
			log.Info("Reconciliation timed out")
		}
	}()

	var rd envoyxdsv1alpha1.Route
	routeConfigFound := true
	if err := r.Get(ctx, req.NamespacedName, &rd); err != nil { // nolint
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to get pod")
			return ctrl.Result{}, err
		}
		routeConfigFound = false
	}

	r.Config.RLockConfig()
	previous := r.routePlacement(req.Name)
	r.Config.RUnlockConfig()

	// Routes this one evicted may win now that it changed or is gone.
	defer r.requeueLosers(ctx, req.NamespacedName)

	if !routeConfigFound {
		r.notifyListeners(ctx, req.Namespace, r.removeRouteFromNodes(ctx, req.Name, previous.nodes))
		return ctrl.Result{}, nil
	}

	listenerNames := rd.Spec.ListenerRefs
	if len(listenerNames) == 0 {
		r.unplace(ctx, &rd, previous, "listener_refs not set, route is not served")
		return ctrl.Result{}, nil
	}

	rds, err := RouteRecast(rd.Spec.Route)
	if err != nil {
		log.Error(err, "unable to recast route")
		if strings.Contains(err.Error(), "could not resolve Any message type") {
			log.V(1).Info("you need te add import in rds controller to fix this error, example: 	_ \"github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3\"")
		}
		xds.RecordConfigError(rd.Name, "RDS", "you need te add import in rds controller to fix this error, example: 	_ \"github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3\"")
		if statusErr := r.updateRouteStatus(ctx, &rd, false, nil, listenerNames, 0, err.Error()); statusErr != nil {
			log.Error(statusErr, "unable to update Route status")
		}
		return ctrl.Result{}, nil
	}
	rds.Name = rd.Name

	// An invalid matcher would make Envoy reject the listener, so the last good version keeps serving.
	matcher, err := fcm.Compile(rd.Spec.FilterChainMatch)
	if err != nil {
		errMsg := "invalid filter_chain_match: " + err.Error()
		log.Error(err, "invalid filter_chain_match")
		xds.RecordConfigError(rd.Name, "RDS", errMsg)
		if statusErr := r.updateRouteStatus(ctx, &rd, false, nil, listenerNames, 0, errMsg); statusErr != nil {
			log.Error(statusErr, "unable to update Route status")
		}
		return ctrl.Result{}, nil
	}

	currentNodes := r.getNodesForRoute(ctx, &rd, listenerNames, req)
	if len(currentNodes) == 0 {
		r.unplace(ctx, &rd, previous, "no listener in listener_refs exists on the route's nodes")
		return ctrl.Result{}, nil
	}

	currentNodeSet := make(map[string]struct{}, len(currentNodes))
	for _, node := range currentNodes {
		currentNodeSet[node] = struct{}{}
	}

	nodesToRemove := make(map[string]struct{})
	for nodeID := range previous.nodes {
		if _, exists := currentNodeSet[nodeID]; !exists {
			nodesToRemove[nodeID] = struct{}{}
		}
	}
	r.notifyListeners(ctx, req.Namespace, r.removeRouteFromNodes(ctx, req.Name, nodesToRemove))

	if listener := r.staticChainConflict(ctx, &rd, matcher, currentNodeSet); listener != "" {
		r.reject(ctx, &rd, previous, fmt.Sprintf("filter chain duplicates one defined in Listener '%s' spec.filter_chains", listener))
		return ctrl.Result{}, nil
	}

	r.Config.RLockConfig()
	conflicts := r.findConflicts(&rd, matcher, currentNodeSet)
	r.Config.RUnlockConfig()

	if len(conflicts) > 0 && util.OlderRoute(conflicts[0].route, &rd) {
		for _, c := range conflicts {
			if util.OlderRoute(c.route, &rd) {
				r.addLoser(client.ObjectKeyFromObject(c.route), req.NamespacedName)
			}
		}
		winner := conflicts[0]
		r.reject(ctx, &rd, previous, fmt.Sprintf("filter chain conflict with older route '%s' (created %s): %s",
			winner.route.Name, winner.route.CreationTimestamp.Format(time.RFC3339), conflictReason(winner.verdict)))
		return ctrl.Result{}, nil
	}

	for _, c := range conflicts {
		errMsg := fmt.Sprintf("route removed: filter chain conflict with older route '%s': %s", rd.Name, conflictReason(c.verdict))
		log.Error(fmt.Errorf("%s", errMsg), "filter chain match conflict", "evicted", c.route.Name)
		xds.RecordConfigError(c.route.Name, "RDS", errMsg)
		r.notifyListeners(ctx, c.route.Namespace, r.removeRouteEverywhere(ctx, c.route.Name))
		r.enqueueRoute(ctx, client.ObjectKeyFromObject(c.route))
	}

	var updated bool
	for _, node := range currentNodes {
		updated = r.updateRouteConfig(node, listenerNames, &rd, rds, matcher)
	}
	if !previous.same(currentNodeSet, listenerNames, rd.Generation) {
		r.notifyListeners(ctx, req.Namespace, mergeNames(previous.listeners, listenerNames))
	}

	if updated {
		log.V(2).Info("Updated route")
	} else {
		log.V(2).Info("Added route")
	}

	virtualHostCount := 0
	if rd.Spec.RouteConfig != nil && rd.Spec.RouteConfig.VirtualHosts != nil {
		virtualHostCount = len(rd.Spec.RouteConfig.VirtualHosts)
	}

	if statusErr := r.updateRouteStatus(ctx, &rd, true, currentNodes, listenerNames, virtualHostCount, ""); statusErr != nil {
		log.Error(statusErr, "unable to update Route status")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Add a Runnable to initialize total count after cache sync
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		log := ctrl.Log.WithName("RDS")

		// Wait for cache to sync
		if !mgr.GetCache().WaitForCacheSync(ctx) {
			return fmt.Errorf("failed to sync cache")
		}

		// Now it's safe to list resources
		var routeList envoyxdsv1alpha1.RouteList
		if err := r.List(ctx, &routeList); err != nil {
			return fmt.Errorf("unable to list Routes: %w", err)
		}

		// Initialize reconciliation status
		count := len(routeList.Items)
		log.Info("Initializing RDS controller", "resources", count)
		if count > 0 {
			r.Config.ReconciliationStatus.SetHasRoutes(true)
			log.Info("RDS reconciliation starting", "resources", count)
		} else {
			log.Info("RDS reconciliation complete", "resources", 0)
		}
		// Mark routes controller as initialized
		r.Config.ReconciliationStatus.SetRoutesInitialized(true)
		return nil
	})); err != nil {
		return err
	}

	r.requeue = make(chan event.GenericEvent, 256)

	return ctrl.NewControllerManagedBy(mgr).
		For(&envoyxdsv1alpha1.Route{}).
		WatchesRawSource(source.Channel(r.requeue, &handler.EnqueueRequestForObject{})).
		Watches(&envoyxdsv1alpha1.Listener{}, handler.EnqueueRequestsFromMapFunc(r.routesForListener)).
		Complete(r)
}

// routesForListener maps a Listener event, including its deletion, to the routes that reference it.
func (r *RouteReconciler) routesForListener(ctx context.Context, listener client.Object) []reconcile.Request {
	var routeList envoyxdsv1alpha1.RouteList
	if err := r.List(ctx, &routeList, client.InNamespace(listener.GetNamespace())); err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to list routes")
		return nil
	}
	var requests []reconcile.Request
	for _, route := range routeList.Items {
		if slices.Contains(route.Spec.ListenerRefs, listener.GetName()) {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&route)})
		}
	}
	return requests
}

// RouteRecast converts Route types to Envoy RouteConfiguration.
func RouteRecast(r routetypes.Route) (*routev3.RouteConfiguration, error) {
	routeConfig := &routev3.RouteConfiguration{}

	// Marshal route config to json
	routeData, err := json.Marshal(r.RouteConfig)
	if err != nil {
		return nil, fmt.Errorf("JSON marshaling failed: %w", err)
	}

	// Unmarshal RouteConfiguration from json
	err = protojson.Unmarshal(routeData, routeConfig)
	if err != nil {
		return nil, fmt.Errorf("proto unmarshaling failed: %w", err)
	}

	return routeConfig, nil
}

type routeConflict struct {
	route   *envoyxdsv1alpha1.Route
	verdict fcm.Verdict
}

// RouteConflict reports how the filter chains of routes a and b collide once placed on a shared node.
func RouteConflict(a, b *envoyxdsv1alpha1.Route, ma, mb *fcm.Matcher) fcm.Verdict {
	if !sharesName(a.Spec.ListenerRefs, b.Spec.ListenerRefs) {
		return fcm.None
	}
	v := ma.Compare(mb)
	if v == fcm.Shadow && !virtualHostsOverlap(a.Spec.RouteConfig, b.Spec.RouteConfig) {
		return fcm.None
	}
	return v
}

func conflictReason(v fcm.Verdict) string {
	if v == fcm.Shadow {
		return "server_names overlap through a wildcard and virtual host domains overlap"
	}
	return "Envoy rejects a listener holding both filter chains (same matching rules)"
}

func virtualHostsOverlap(a, b *rdstypes.RDS) bool {
	if a == nil || b == nil {
		return false
	}
	for _, vh1 := range a.VirtualHosts {
		for _, vh2 := range b.VirtualHosts {
			if hasVirtualHostOverlap(vh1, vh2) {
				return true
			}
		}
	}
	return false
}

func hasVirtualHostOverlap(vh1, vh2 *rdstypes.VirtualHost) bool {
	if vh1 == nil || vh2 == nil {
		return false
	}
	return sharesName(vh1.Domains, vh2.Domains)
}

func sharesName(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// findConflicts returns routes on the given nodes whose chains cannot coexist with route's,
// oldest first. Caller holds the config read lock.
func (r *RouteReconciler) findConflicts(route *envoyxdsv1alpha1.Route, m *fcm.Matcher, nodes map[string]struct{}) []routeConflict {
	var out []routeConflict
	seen := make(map[string]struct{})
	for node := range nodes {
		for _, rc := range r.Config.RouteConfigs[node] {
			name := rc.Route.Name
			if name == route.Name {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}

			other := rc.Matcher
			if other == nil {
				var err error
				if other, err = fcm.Compile(rc.Route.Spec.FilterChainMatch); err != nil {
					continue
				}
			}
			if v := RouteConflict(route, rc.Route, m, other); v != fcm.None {
				out = append(out, routeConflict{route: rc.Route, verdict: v})
			}
		}
	}
	slices.SortFunc(out, func(x, y routeConflict) int {
		switch {
		case util.OlderRoute(x.route, y.route):
			return -1
		case util.OlderRoute(y.route, x.route):
			return 1
		}
		return 0
	})
	return out
}

// placement is where a route was served before this reconcile.
type placement struct {
	nodes      map[string]struct{}
	listeners  []string
	generation int64
}

func (p placement) same(nodes map[string]struct{}, listeners []string, generation int64) bool {
	if p.generation != generation || len(p.nodes) != len(nodes) || !slices.Equal(p.listeners, mergeNames(listeners, nil)) {
		return false
	}
	for n := range nodes {
		if _, ok := p.nodes[n]; !ok {
			return false
		}
	}
	return true
}

// routePlacement returns where the named route is served. Caller holds the config read lock.
func (r *RouteReconciler) routePlacement(name string) placement {
	p := placement{nodes: make(map[string]struct{})}
	for nodeID, routes := range r.Config.RouteConfigs {
		for _, rc := range routes {
			if rc.Route.Name == name {
				p.nodes[nodeID] = struct{}{}
				p.listeners = mergeNames(p.listeners, rc.ListenerNames)
				p.generation = rc.Route.Generation
				break
			}
		}
	}
	return p
}

// unplace stops serving the route and reports why on its status.
func (r *RouteReconciler) unplace(ctx context.Context, route *envoyxdsv1alpha1.Route, previous placement, msg string) {
	r.notifyListeners(ctx, route.Namespace, r.removeRouteFromNodes(ctx, route.Name, previous.nodes))
	if err := r.updateRouteStatus(ctx, route, false, nil, route.Spec.ListenerRefs, 0, msg); err != nil {
		ctrllog.FromContext(ctx).Error(err, "unable to update Route status")
	}
}

// reject removes the route from every node because its filter chain cannot be served.
func (r *RouteReconciler) reject(ctx context.Context, route *envoyxdsv1alpha1.Route, previous placement, msg string) {
	ctrllog.FromContext(ctx).Error(fmt.Errorf("%s", msg), "filter chain match conflict")
	xds.RecordConfigError(route.Name, "RDS", msg)
	removed := r.removeRouteEverywhere(ctx, route.Name)
	r.notifyListeners(ctx, route.Namespace, mergeNames(previous.listeners, removed))
	if err := r.updateRouteStatus(ctx, route, false, nil, route.Spec.ListenerRefs, 0, msg); err != nil {
		ctrllog.FromContext(ctx).Error(err, "unable to update Route status")
	}
}

// staticChainConflict returns the listener whose own filter_chains Envoy cannot tell apart
// from the route's chain on a node the route is placed on.
func (r *RouteReconciler) staticChainConflict(ctx context.Context, route *envoyxdsv1alpha1.Route, m *fcm.Matcher, nodes map[string]struct{}) string {
	for _, name := range route.Spec.ListenerRefs {
		var l envoyxdsv1alpha1.Listener
		if err := r.Get(ctx, types.NamespacedName{Namespace: route.Namespace, Name: name}, &l); err != nil || len(l.Spec.FilterChains) == 0 {
			continue
		}
		onNode := false
		for _, id := range util.NodeIDs(l.Annotations, r.Config.NodeID, r.Config.Cluster) {
			if _, ok := nodes[id]; ok {
				onNode = true
				break
			}
		}
		if !onNode {
			continue
		}
		for _, fc := range l.Spec.FilterChains {
			if fc == nil {
				continue
			}
			if sm, err := fcm.Compile(fc.FilterChainMatch); err == nil && m.Compare(sm) == fcm.Duplicate {
				return name
			}
		}
	}
	return ""
}

func (r *RouteReconciler) getNodesForRoute(ctx context.Context, route *envoyxdsv1alpha1.Route, listenerNames []string, req ctrl.Request) []string {
	if route.Annotations == nil {
		route.Annotations = make(map[string]string)
	}
	if route.Annotations["nodes"] == "" {
		route.Annotations["nodes"] = r.Config.NodeID
	}
	if route.Annotations["clusters"] == "" {
		route.Annotations["clusters"] = r.Config.Cluster
	}
	nodes := util.NodeIDs(route.Annotations, r.Config.NodeID, r.Config.Cluster)

	isLiExists, unmatchedNodes := r.isListenerExists(ctx, listenerNames, nodes, req)
	if !isLiExists {
		return []string{}
	}

	if len(unmatchedNodes) > 0 {
		newNodes := []string{}
		for _, node := range nodes {
			if !isContains(unmatchedNodes, node) {
				newNodes = append(newNodes, node)
			}
		}
		nodes = newNodes
	}

	return nodes
}

// removeRouteFromNodes removes the route from the given nodes and returns the listeners it was attached to.
func (r *RouteReconciler) removeRouteFromNodes(ctx context.Context, routeName string, nodes map[string]struct{}) []string {
	if len(nodes) == 0 {
		return nil
	}
	r.Config.LockConfig()
	defer r.Config.UnlockConfig()
	var listeners []string
	for nodeID := range nodes {
		listeners = r.removeRouteLocked(nodeID, routeName, listeners)
	}
	if len(listeners) > 0 {
		ctrllog.FromContext(ctx).V(0).Info("Removed route", "route", routeName)
	}
	return listeners
}

// removeRouteEverywhere removes the route from every node and returns the listeners it was attached to.
func (r *RouteReconciler) removeRouteEverywhere(ctx context.Context, routeName string) []string {
	r.Config.LockConfig()
	defer r.Config.UnlockConfig()
	var listeners []string
	for nodeID := range r.Config.RouteConfigs {
		listeners = r.removeRouteLocked(nodeID, routeName, listeners)
	}
	if len(listeners) > 0 {
		ctrllog.FromContext(ctx).V(0).Info("Removed route", "route", routeName)
	}
	return listeners
}

func (r *RouteReconciler) removeRouteLocked(nodeID, routeName string, listeners []string) []string {
	routes := r.Config.RouteConfigs[nodeID]
	for i, rc := range routes {
		if rc.Route.Name == routeName {
			r.Config.RouteConfigs[nodeID] = slices.Delete(routes, i, i+1)
			r.Config.IncrementConfigCounter()
			return mergeNames(listeners, rc.ListenerNames)
		}
	}
	return listeners
}

func (r *RouteReconciler) updateRouteConfig(node string, listenerNames []string, route *envoyxdsv1alpha1.Route, rd *routev3.RouteConfiguration, matcher *fcm.Matcher) bool {
	r.Config.LockConfig()
	defer r.Config.UnlockConfig()
	if r.Config.RouteConfigs == nil {
		r.Config.RouteConfigs = make(map[string][]*xds.RouteConfig)
	}

	rc := &xds.RouteConfig{RouteConfiguration: rd, ListenerNames: listenerNames, Route: route, Matcher: matcher}
	r.Config.IncrementConfigCounter()
	for i, ro := range r.Config.RouteConfigs[node] {
		if ro.Route.Name == route.Name {
			r.Config.RouteConfigs[node][i] = rc
			return true
		}
	}
	r.Config.RouteConfigs[node] = append(r.Config.RouteConfigs[node], rc)
	return false
}

// notifyListeners asks LDS to rebuild listeners whose filter chains depend on routes this reconcile changed.
func (r *RouteReconciler) notifyListeners(ctx context.Context, namespace string, names []string) {
	if r.ListenerEvents == nil || len(names) == 0 {
		return
	}
	r.Config.ReconciliationStatus.SetListenersReconciled(false)
	for _, name := range names {
		key := types.NamespacedName{Namespace: namespace, Name: name}
		r.Config.ReconciliationStatus.MarkListenerPending(key.String())
		select {
		case r.ListenerEvents <- event.GenericEvent{Object: &envoyxdsv1alpha1.Listener{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		}}:
		case <-ctx.Done():
			return
		}
	}
}

func (r *RouteReconciler) enqueueRoute(ctx context.Context, key types.NamespacedName) {
	if r.requeue == nil {
		return
	}
	select {
	case r.requeue <- event.GenericEvent{Object: &envoyxdsv1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
	}}:
	case <-ctx.Done():
	}
}

func (r *RouteReconciler) addLoser(winner, loser types.NamespacedName) {
	r.losersMu.Lock()
	defer r.losersMu.Unlock()
	if r.losers == nil {
		r.losers = make(map[types.NamespacedName]map[types.NamespacedName]struct{})
	}
	if r.losers[winner] == nil {
		r.losers[winner] = make(map[types.NamespacedName]struct{})
	}
	r.losers[winner][loser] = struct{}{}
}

func (r *RouteReconciler) requeueLosers(ctx context.Context, winner types.NamespacedName) {
	r.losersMu.Lock()
	losers := r.losers[winner]
	delete(r.losers, winner)
	r.losersMu.Unlock()
	for key := range losers {
		r.enqueueRoute(ctx, key)
	}
}

func mergeNames(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	slices.Sort(out)
	return slices.Compact(out)
}

func (r *RouteReconciler) isListenerExists(ctx context.Context, listenerNames, nodes []string, req ctrl.Request) (exists bool, unmatchedNodes []string) {
	var li envoyxdsv1alpha1.Listener
	listeners := listenerNames
	unmatchedNodes = make([]string, 0)

	exists = false

	// Now check each node against each listener for matches
	for _, node := range nodes {
		nodeInfo, _ := util.GetNodeInfo(node) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore
		nodeMatched := false

		for _, listenerName := range listeners {
			if err := r.Get(ctx, types.NamespacedName{Name: listenerName, Namespace: req.Namespace}, &li); err == nil {
				exists = true
			}

			// Set default nodes and clusters if not present
			if li.Annotations == nil {
				li.Annotations = make(map[string]string)
			}
			if li.Annotations["nodes"] == "" {
				li.Annotations["nodes"] = r.Config.NodeID
			}
			if li.Annotations["clusters"] == "" {
				li.Annotations["clusters"] = r.Config.Cluster
			}

			// Get listener's node ID and verify it matches
			listenerNodeID := util.GetNodeID(li.Annotations)
			listenerNodeInfo, _ := util.GetNodeInfo(listenerNodeID) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore

			// Check if nodes and clusters match
			nodesMatch := false
			clustersMatch := false

			for _, n := range nodeInfo.Nodes {
				if isContains(listenerNodeInfo.Nodes, n) {
					nodesMatch = true
					break
				}
			}

			for _, c := range nodeInfo.Clusters {
				if isContains(listenerNodeInfo.Clusters, c) {
					clustersMatch = true
					break
				}
			}

			if nodesMatch && clustersMatch {
				nodeMatched = true
				break
			}
		}

		if !nodeMatched {
			unmatchedNodes = append(unmatchedNodes, node)
		}
	}

	return exists, unmatchedNodes
}

func isContains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// updateRouteStatus updates the status of the Route CR
func (r *RouteReconciler) updateRouteStatus(ctx context.Context, routeCR *envoyxdsv1alpha1.Route, active bool, activeNodes, listeners []string, virtualHostCount int, message string) error {
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
	routeKey := types.NamespacedName{Name: routeCR.Name, Namespace: routeCR.Namespace}
	generation := routeCR.Generation

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch the latest version of the Route to get the current resourceVersion
		var latestRoute envoyxdsv1alpha1.Route
		if err := r.Get(ctx, routeKey, &latestRoute); err != nil {
			return err
		}

		// Build conditions from the latest route's conditions
		conditions := latestRoute.Status.Conditions
		if conditions == nil {
			conditions = []metav1.Condition{}
		}

		// Update Ready condition
		readyCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.RouteConditionReady,
			LastTransitionTime: now,
			ObservedGeneration: generation,
		}
		if active {
			readyCondition.Status = metav1.ConditionTrue
			readyCondition.Reason = "Active"
			readyCondition.Message = fmt.Sprintf("Route is active in %d snapshots", len(snapshots))
		} else {
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = "Inactive"
			readyCondition.Message = message
		}
		conditions = updateRouteCondition(conditions, readyCondition)

		// Update Reconciled condition
		reconciledCondition := metav1.Condition{
			Type:               envoyxdsv1alpha1.RouteConditionReconciled,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             "Reconciled",
			Message:            "Successfully reconciled",
			ObservedGeneration: generation,
		}
		conditions = updateRouteCondition(conditions, reconciledCondition)

		// Update Error condition if there's an error
		if message != "" && !active {
			errorCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.RouteConditionError,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "Error",
				Message:            message,
				ObservedGeneration: generation,
			}
			conditions = updateRouteCondition(conditions, errorCondition)
		} else {
			// Clear error condition
			errorCondition := metav1.Condition{
				Type:               envoyxdsv1alpha1.RouteConditionError,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: now,
				Reason:             "NoError",
				Message:            "",
				ObservedGeneration: generation,
			}
			conditions = updateRouteCondition(conditions, errorCondition)
		}

		// Prepare the new status
		newStatus := envoyxdsv1alpha1.RouteStatus{
			Active:             active,
			VirtualHostCount:   virtualHostCount,
			Listeners:          strings.Join(listeners, ","),
			Snapshots:          snapshots,
			Nodes:              strings.Join(nodesList, ","),
			Clusters:           strings.Join(clustersList, ","),
			LastReconciled:     now,
			ObservedGeneration: generation,
			Conditions:         conditions,
			Message:            message,
		}

		// Update status if changed
		if !routeStatusEqual(latestRoute.Status, newStatus) {
			latestRoute.Status = newStatus
			if err := r.Status().Update(ctx, &latestRoute); err != nil {
				return err
			}
			log.V(2).Info("Updated route status", "active", active, "virtualHosts", virtualHostCount, "nodes", strings.Join(nodesList, ","))
		}

		return nil
	})
}

// updateRouteCondition updates or adds a condition to the conditions slice
func updateRouteCondition(conditions []metav1.Condition, newCondition metav1.Condition) []metav1.Condition {
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

// routeStatusEqual compares two RouteStatus objects (ignoring LastReconciled time for comparison)
func routeStatusEqual(a, b envoyxdsv1alpha1.RouteStatus) bool {
	if a.Active != b.Active {
		return false
	}
	if a.VirtualHostCount != b.VirtualHostCount {
		return false
	}
	if a.Listeners != b.Listeners {
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
	if a.Message != b.Message {
		return false
	}
	if len(a.Snapshots) != len(b.Snapshots) {
		return false
	}
	return true
}
