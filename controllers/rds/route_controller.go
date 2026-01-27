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
	"net/url"
	"sort"
	"strings"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
	rdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/rds"
	routetypes "github.com/tentens-tech/xds-controller/pkg/xds/types/route"
)

// RouteReconciler reconciles a Route object
type RouteReconciler struct {
	client.Client
	Scheme                 *runtime.Scheme
	Config                 *xds.Config
	reconciling            atomic.Int32
	lastReconcileTime      atomic.Int64
	initialReconcileLogged atomic.Bool
}

//+kubebuilder:rbac:groups=envoyxds.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=envoyxds.io,resources=routes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=envoyxds.io,resources=routes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Route object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *RouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	r.Config.ReconciliationStatus.SetRoutesReconciled(false)
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

	// Build the set of previous nodes where the routes exists
	previousNodeSet := make(map[string]struct{})
	for nodeID, routes := range r.Config.RouteConfigs {
		for _, r := range routes {
			if r.Route.Name == req.Name {
				previousNodeSet[nodeID] = struct{}{}
				break
			}
		}
	}

	// If route not found, remove it and mark as processed
	if !routeConfigFound {
		r.removeRouteFromNodes(ctx, req.Name, previousNodeSet)
		return ctrl.Result{}, nil
	}

	// Get listener names from spec.listener_refs
	listenerNames := rd.Spec.ListenerRefs
	if len(listenerNames) == 0 {
		log.V(1).WithName(req.Name).Info("listener_refs not set on route")
		return ctrl.Result{}, nil
	}

	currentNodes := r.getNodesForRoute(ctx, &rd, listenerNames, req)

	if len(currentNodes) == 0 {
		r.removeRouteFromNodes(ctx, req.Name, previousNodeSet)
		return ctrl.Result{}, nil
	}

	// Build the set of current nodes
	currentNodeSet := make(map[string]struct{})
	for _, node := range currentNodes {
		currentNodeSet[node] = struct{}{}
	}

	// Remove routes from nodes where it no longer belongs
	nodesToRemove := make(map[string]struct{})
	for nodeID := range previousNodeSet {
		if _, exists := currentNodeSet[nodeID]; !exists {
			nodesToRemove[nodeID] = struct{}{}
			delete(currentNodeSet, nodeID)
		}
	}
	r.removeRouteFromNodes(ctx, req.Name, nodesToRemove)

	rds, err := RouteRecast(rd.Spec.Route)
	if err != nil {
		log.Error(err, "unable to recast route")
		if strings.Contains(err.Error(), "could not resolve Any message type") {
			log.V(1).Info("you need te add import in rds controller to fix this error, example: 	_ \"github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3\"")
		}
		xds.RecordConfigError(rd.Name, "RDS", "you need te add import in rds controller to fix this error, example: 	_ \"github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3\"")
		// Update status with error
		if statusErr := r.updateRouteStatus(ctx, &rd, false, nil, listenerNames, 0, err.Error()); statusErr != nil {
			log.Error(statusErr, "unable to update Route status")
		}
		return ctrl.Result{}, nil
	}

	rds.Name = rd.Name
	var currentMatch *lds.FilterChainMatch
	if rd.Spec.FilterChainMatch != nil {
		currentMatch = rd.Spec.FilterChainMatch
	}
	currentTime := rd.CreationTimestamp.Time

	// Validate domains against existing RouteConfigurations
	for node := range r.Config.RouteConfigs {
		nodeInfo, _ := util.GetNodeInfo(node) //nolint:errcheck // GetNodeInfo returns empty struct on error, safe to ignore
		for _, existingRC := range r.Config.RouteConfigs[node] {
			if existingRC.Route.Name == rds.Name {
				continue // Skip comparing with itself
			}

			// Get the existing route to check its timestamp
			existingRoute := *existingRC.Route

			existingTime := existingRoute.CreationTimestamp.Time
			existingMatch := existingRoute.Spec.FilterChainMatch

			// First check filter chain overlap
			if hasFilterChainOverlap(existingMatch, currentMatch) {
				// Then check if the virtual hosts actually overlap
				if hasRouteConfigOverlap(&existingRoute.Spec.Route, &rd.Spec.Route, existingMatch, currentMatch) {
					if currentTime.After(existingTime) {
						// Current route is newer - return error
						errMsg := fmt.Sprintf(
							"filter chain match overlap detected: route '%s' (created: %s) conflicts with older route '%s' (created: %s) in nodes %v, clusters %v.\nConflicting criteria:\nServerNames: %v\n",
							rds.Name,
							currentTime.Format(time.RFC3339),
							existingRC.RouteConfiguration.Name,
							existingTime.Format(time.RFC3339),
							nodeInfo.Nodes,
							nodeInfo.Clusters,
							currentMatch.ServerNames,
						)
						r.Config.LockConfig()
						r.Config.RouteConfigs[node] = r.deleteRouteConfig(node, rds.Name)
						r.Config.UnlockConfig()
						log.Error(fmt.Errorf("%s", errMsg), "filter chain match conflict")
						xds.RecordConfigError(rds.Name, "RDS", errMsg)
						// Update status with error
						if statusErr := r.updateRouteStatus(ctx, &rd, false, nil, listenerNames, 0, errMsg); statusErr != nil {
							log.Error(statusErr, "unable to update Route status")
						}
						return ctrl.Result{}, nil
					} else {
						// Current route is older - remove newer route and continue with adding this one
						log.V(1).Info(fmt.Sprintf("removing newer route '%s' as it conflicts with older route '%s'", existingRC.Route.Name, rds.Name))
						r.Config.LockConfig()
						r.Config.RouteConfigs[node] = r.deleteRouteConfig(node, existingRC.Route.Name)
						r.Config.UnlockConfig()

						// Record error for the removed route
						errMsg := fmt.Sprintf(
							"route removed due to filter chain match overlap with older route '%s' (created: %s) match %v",
							rds.Name,
							currentTime.Format(time.RFC3339),
							currentMatch,
						)
						log.Error(fmt.Errorf("%s", errMsg), "filter chain match conflict")
						xds.RecordConfigError(existingRC.RouteConfiguration.Name, "RDS", errMsg)
					}
				}
			}
		}
	}

	var updated bool
	// Update or add routes to the current nodes
	for _, node := range currentNodes {
		updated = r.updateRouteConfig(node, listenerNames, &rd, rds)
	}

	if updated {
		log.V(2).Info("Updated route")
	} else {
		log.V(2).Info("Added route")
	}

	// Count virtual hosts
	virtualHostCount := 0
	if rd.Spec.RouteConfig != nil && rd.Spec.RouteConfig.VirtualHosts != nil {
		virtualHostCount = len(rd.Spec.RouteConfig.VirtualHosts)
	}

	// Update status
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

	return ctrl.NewControllerManagedBy(mgr).
		For(&envoyxdsv1alpha1.Route{}).
		Watches(&envoyxdsv1alpha1.Listener{}, handler.EnqueueRequestsFromMapFunc(
			func(ctx context.Context, a client.Object) []reconcile.Request {
				listenerObj, ok := a.(*envoyxdsv1alpha1.Listener)
				if !ok {
					return nil
				}
				log := ctrllog.FromContext(ctx)

				// Check if the listener still exists
				var existingListener envoyxdsv1alpha1.Listener
				err := r.Get(context.Background(), types.NamespacedName{
					Name:      listenerObj.Name,
					Namespace: listenerObj.Namespace,
				}, &existingListener)

				if err != nil || apierrors.IsNotFound(err) {
					return nil
				}

				// Get routes that reference this listener
				var routeList envoyxdsv1alpha1.RouteList
				if err := r.List(context.Background(), &routeList, &client.ListOptions{
					Namespace: a.GetNamespace(),
				}); err != nil {
					log.Error(err, "Failed to list routes")
					return nil
				}

				var requests []reconcile.Request
				// Check each route for the listener name
				for _, route := range routeList.Items {
					if len(route.Spec.ListenerRefs) > 0 {
						for _, name := range route.Spec.ListenerRefs {
							if name == listenerObj.Name {
								requests = append(requests, reconcile.Request{
									NamespacedName: types.NamespacedName{
										Name:      route.Name,
										Namespace: route.Namespace,
									},
								})
								break
							}
						}
					}
				}

				return requests
			}),
		).
		Complete(r)
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

func matchDomainName(pattern, domain string) bool {
	// Clean up the domains by removing any trailing dots
	pattern = strings.TrimSuffix(pattern, ".")
	domain = strings.TrimSuffix(domain, ".")

	if pattern == domain {
		return true
	}

	// Handle wildcard domains
	if strings.HasPrefix(pattern, "*.") {
		// For wildcard match, we need to:
		// 1. Extract the base domain after *. (e.g., "*.example.com" -> "example.com")
		// 2. Ensure the domain has exactly one subdomain level
		// 3. Check if the base domains match
		baseDomain := pattern[2:] // Skip the *. part
		domainParts := strings.Split(domain, ".")

		// Must have at least two parts (subdomain + base domain)
		if len(domainParts) < 2 {
			return false
		}

		// Get everything after the first part
		domainSuffix := strings.Join(domainParts[1:], ".")
		return domainSuffix == baseDomain
	}

	// Parse URLs to compare hosts
	patternURL, err := url.Parse("https://" + pattern)
	if err != nil {
		return false
	}

	domainURL, err := url.Parse("https://" + domain)
	if err != nil {
		return false
	}

	return patternURL.Hostname() == domainURL.Hostname()
}

func hasVirtualHostOverlap(vh1, vh2 *rdstypes.VirtualHost) bool {
	if vh1 == nil || vh2 == nil {
		return false
	}

	// Create domain sets for comparison
	domains1 := make(map[string]bool)
	domains2 := make(map[string]bool)

	for _, domain := range vh1.Domains {
		domains1[domain] = true
	}
	for _, domain := range vh2.Domains {
		domains2[domain] = true
	}

	// Check for any overlapping domains
	for domain := range domains1 {
		if domains2[domain] {
			return true
		}
	}

	return false
}

func hasRouteConfigOverlap(route1, route2 *routetypes.Route, match1, match2 *lds.FilterChainMatch) bool {
	if route1 == nil || route2 == nil ||
		route1.RouteConfig == nil || route2.RouteConfig == nil {
		return false
	}

	// Check if either match has wildcards
	hasWildcard := func(match *lds.FilterChainMatch) bool {
		if match == nil || len(match.ServerNames) == 0 {
			return false
		}
		for _, name := range match.ServerNames {
			if strings.Contains(name, "*") {
				return true
			}
		}
		return false
	}

	// If either match has wildcards, we should focus on checking VirtualHosts
	if hasWildcard(match1) || hasWildcard(match2) {
		// Check virtual host overlaps
		for _, vh1 := range route1.RouteConfig.VirtualHosts {
			for _, vh2 := range route2.RouteConfig.VirtualHosts {
				if hasVirtualHostOverlap(vh1, vh2) {
					return true
				}
			}
		}
		return false
	}

	// For non-wildcard matches, check filter chain overlap first
	if hasFilterChainOverlap(match1, match2) {
		return true
	}

	return false
}

func hasFilterChainOverlap(match1, match2 *lds.FilterChainMatch) bool {
	// If either match is nil, they don't overlap (different types of routes)
	if match1 == nil || match2 == nil {
		return false
	}

	// Check if routes are of the same type by checking which fields are set
	hasServerNames1 := len(match1.ServerNames) > 0
	hasServerNames2 := len(match2.ServerNames) > 0
	if hasServerNames1 != hasServerNames2 {
		return false
	}

	hasSourcePrefix1 := len(match1.SourcePrefixRanges) > 0
	hasSourcePrefix2 := len(match2.SourcePrefixRanges) > 0
	if hasSourcePrefix1 != hasSourcePrefix2 {
		return false
	}

	// Now check overlaps based on the type of route

	// Check destination port overlap (always check if specified)
	if match1.DestinationPort != nil && match2.DestinationPort != nil && *match1.DestinationPort != *match2.DestinationPort {
		return false
	}

	// Check ServerNames overlap
	if hasServerNames1 {
		for _, name1 := range match1.ServerNames {
			for _, name2 := range match2.ServerNames {
				if matchDomainName(name1, name2) || matchDomainName(name2, name1) {
					return true
				}
			}
		}
		return false
	}

	// Check SourcePrefixRanges overlap
	if hasSourcePrefix1 {
		for _, prefix1 := range match1.SourcePrefixRanges {
			for _, prefix2 := range match2.SourcePrefixRanges {
				if prefix1.AddressPrefix == prefix2.AddressPrefix && prefix1.PrefixLen == prefix2.PrefixLen {
					return true
				}
			}
		}
		return false
	}

	// If we get here, it means both matches are empty or have the same empty fields
	return true
}

// deleteRouteConfig removes a route configuration by name from the specified node
// and returns the updated route configurations
func (r *RouteReconciler) deleteRouteConfig(node, routeName string) []*xds.RouteConfig {
	routes := r.Config.RouteConfigs[node]
	for i := len(routes) - 1; i >= 0; i-- {
		route := routes[i]
		if route.Route.Name == routeName {
			r.Config.RouteConfigs[node] = append(routes[:i], routes[i+1:]...)
			r.Config.IncrementConfigCounter()
			break
		}
	}
	return r.Config.RouteConfigs[node]
}

func (r *RouteReconciler) getNodesForRoute(ctx context.Context, route *envoyxdsv1alpha1.Route, listenerNames []string, req ctrl.Request) []string {
	nodes := []string{}

	// Set default nodes and clusters if not present
	if route.Annotations == nil {
		route.Annotations = make(map[string]string)
	}
	if route.Annotations["nodes"] == "" {
		route.Annotations["nodes"] = r.Config.NodeID
	}
	if route.Annotations["clusters"] == "" {
		route.Annotations["clusters"] = r.Config.Cluster
	}

	// Parse nodes and clusters from annotations
	nodesList := util.ParseCSV(route.Annotations["nodes"])
	clustersList := util.ParseCSV(route.Annotations["clusters"])

	// Sort nodes and clusters for consistent NodeID generation
	sort.Strings(nodesList)
	sort.Strings(clustersList)

	for _, cluster := range clustersList {
		for _, node := range nodesList {
			nodeID := util.GetNodeID(map[string]string{"clusters": cluster, "nodes": node})
			nodes = append(nodes, nodeID)
		}
	}

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

func (r *RouteReconciler) removeRouteFromNodes(ctx context.Context, routeName string, nodes map[string]struct{}) {
	log := ctrllog.FromContext(ctx)
	var removed bool
	for nodeID := range nodes {
		for i := len(r.Config.RouteConfigs[nodeID]) - 1; i >= 0; i-- {
			route := r.Config.RouteConfigs[nodeID][i]
			if route.Route.Name == routeName {
				r.Config.LockConfig()
				r.Config.RouteConfigs[nodeID] = append(r.Config.RouteConfigs[nodeID][:i], r.Config.RouteConfigs[nodeID][i+1:]...)
				removed = true
				r.Config.UnlockConfig()
				r.Config.IncrementConfigCounter()
			}
		}
	}

	if removed {
		log.V(0).Info("Removed route")
	}
}

func (r *RouteReconciler) updateRouteConfig(node string, listenerNames []string, route *envoyxdsv1alpha1.Route, rd *routev3.RouteConfiguration) bool {
	r.Config.LockConfig()
	defer r.Config.UnlockConfig()
	if r.Config.RouteConfigs == nil {
		r.Config.RouteConfigs = make(map[string][]*xds.RouteConfig)
	}

	var updated bool
	for i, ro := range r.Config.RouteConfigs[node] {
		if ro.Route.Name == route.Name {
			r.Config.RouteConfigs[node][i] = &xds.RouteConfig{RouteConfiguration: rd, ListenerNames: listenerNames, Route: route}
			updated = true
			r.Config.IncrementConfigCounter()
			break
		}
	}

	if !updated {
		r.Config.RouteConfigs[node] = append(r.Config.RouteConfigs[node], &xds.RouteConfig{RouteConfiguration: rd, ListenerNames: listenerNames, Route: route})
		r.Config.IncrementConfigCounter()
	}

	return updated
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
	if len(a.Snapshots) != len(b.Snapshots) {
		return false
	}
	return true
}
