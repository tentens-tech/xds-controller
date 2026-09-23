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

// Package xds provides xDS (Envoy Discovery Service) configuration management.
// It handles snapshot generation, caching, and resource management for Envoy proxies.
package xds

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	vault "github.com/hashicorp/vault/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds/fcm"
	xdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types"
)

func GenerateSnapshotsV2(ctx context.Context, x *Config) ([]SnapshotConfig, error) {
	// Initialize resources map
	resources := make(map[string]map[string][]types.Resource)

	// Acquire read lock while reading config maps to prevent race with controllers
	x.RLockConfig()
	resources = getResourcesFromSecretConfigs(x, resources)
	resources = getResourcesFromListenerConfigs(x, resources)
	resources = getResourcesFromClusterConfigs(x, resources)
	resources = getResourcesFromRouteConfigs(x, resources)
	resources = getResourcesFromEndpointConfigs(x, resources)
	x.RUnlockConfig()

	// Pre-allocate snapshot slice
	sc := make([]SnapshotConfig, 0, len(resources))

	memo := &x.snapshotMemo
	memo.mu.Lock()
	defer memo.mu.Unlock()
	defer memo.endPass()
	for nodeID, nodeResources := range resources {
		used := routeReferences(nodeResources[resource.ListenerType], &memo.routes)
		pruneUnreferencedRoutes(nodeResources, used)
		version := memo.hashes.resources(nodeResources)
		snap, err := cache.NewSnapshot(version, nodeResources)
		if err != nil {
			return nil, fmt.Errorf("failed to create snapshot for node %s: %w", nodeID, err)
		}
		sc = append(sc, SnapshotConfig{
			NodeID:       nodeID,
			Version:      version,
			Snapshot:     snap,
			Inconsistent: consistent(snap, used),
		})
	}

	return sc, nil
}

// routeReferences returns the route names the listeners' HTTP connection managers use,
// decoding each HTTP connection manager once across passes.
func routeReferences(listeners []types.Resource, memo *passMemo[[]string]) map[string]bool {
	used := make(map[string]bool)
	add := func(fc *listener.FilterChain) {
		for _, f := range fc.GetFilters() {
			tc := f.GetTypedConfig()
			if tc == nil {
				continue
			}
			for _, name := range memo.get(tc, func() []string {
				hcm := resource.GetHTTPConnectionManager(f)
				if hcm == nil {
					return nil
				}
				var names []string
				if name := hcm.GetRds().GetRouteConfigName(); name != "" {
					names = append(names, name)
				}
				for _, sr := range hcm.GetScopedRoutes().GetScopedRouteConfigurationsList().GetScopedRouteConfigurations() {
					names = append(names, sr.GetRouteConfigurationName())
				}
				return names
			}) {
				used[name] = true
			}
		}
	}
	for _, r := range listeners {
		l, ok := r.(*listener.Listener)
		if !ok {
			continue
		}
		for _, fc := range l.GetFilterChains() {
			add(fc)
		}
		if fc := l.GetDefaultFilterChain(); fc != nil {
			add(fc)
		}
	}
	return used
}

// pruneUnreferencedRoutes drops route configurations no listener on the node uses,
// e.g. a route whose filter chain LDS skipped; the snapshot would be inconsistent otherwise.
func pruneUnreferencedRoutes(res map[string][]types.Resource, used map[string]bool) {
	routes := res[resource.RouteType]
	kept := routes[:0]
	for _, r := range routes {
		if used[cache.GetResourceName(r)] {
			kept = append(kept, r)
		}
	}
	res[resource.RouteType] = kept
}

// consistent is Snapshot.Consistent with the route references already known.
func consistent(snap *cache.Snapshot, routes map[string]bool) error {
	clusters := make(map[string]types.ResourceWithTTL)
	for name, c := range snap.GetResources(resource.ClusterType) {
		clusters[name] = types.ResourceWithTTL{Resource: c}
	}
	refs := map[string]map[string]bool{
		resource.EndpointType: cache.GetResourceReferences(clusters)[resource.EndpointType],
		resource.RouteType:    routes,
	}
	for _, typ := range []string{resource.EndpointType, resource.RouteType} {
		items := snap.GetResources(typ)
		if len(refs[typ]) != len(items) {
			return fmt.Errorf("mismatched %q reference and resource lengths: len(%v) != %d", typ, refs[typ], len(items))
		}
		for name := range items {
			if !refs[typ][name] {
				return fmt.Errorf("inconsistent %q reference: %q not listed", typ, name)
			}
		}
	}
	return nil
}

func getResourcesFromSecretConfigs(x *Config, resources map[string]map[string][]types.Resource) map[string]map[string][]types.Resource {
	for s, v := range x.SecretConfigs {
		targets := targetNodes(s, x)
		for i := range v {
			addResource(targets, v[i], resource.SecretType, resources)
		}
	}
	return resources
}

func getResourcesFromListenerConfigs(x *Config, resources map[string]map[string][]types.Resource) map[string]map[string][]types.Resource {
	for s, v := range x.ListenerConfigs {
		targets := targetNodes(s, x)
		for i := range v {
			addResource(targets, v[i], resource.ListenerType, resources)
		}
	}
	return resources
}

func getResourcesFromClusterConfigs(x *Config, resources map[string]map[string][]types.Resource) map[string]map[string][]types.Resource {
	for s, v := range x.ClusterConfigs {
		targets := targetNodes(s, x)
		for i := range v {
			addResource(targets, v[i], resource.ClusterType, resources)
		}
	}
	return resources
}

func getResourcesFromRouteConfigs(x *Config, resources map[string]map[string][]types.Resource) map[string]map[string][]types.Resource {
	for s, v := range x.RouteConfigs {
		targets := targetNodes(s, x)
		for i := range v {
			addResource(targets, v[i].RouteConfiguration, resource.RouteType, resources)
		}
	}
	return resources
}

func getResourcesFromEndpointConfigs(x *Config, resources map[string]map[string][]types.Resource) map[string]map[string][]types.Resource {
	// Build a set of endpoint names that are referenced by EDS-type clusters
	edsEndpointNames := make(map[string]struct{})
	for _, clusters := range x.ClusterConfigs {
		for _, c := range clusters {
			// Check if cluster uses EDS discovery type
			if c.GetType() == cluster.Cluster_EDS {
				// Get the EDS service name - either from eds_cluster_config or cluster name
				serviceName := c.Name
				if c.EdsClusterConfig != nil && c.EdsClusterConfig.ServiceName != "" {
					serviceName = c.EdsClusterConfig.ServiceName
				}
				edsEndpointNames[serviceName] = struct{}{}
			}
		}
	}

	// Only add endpoints that are referenced by EDS-type clusters
	for s, v := range x.EndpointConfigs {
		targets := targetNodes(s, x)
		for i := range v {
			if _, referenced := edsEndpointNames[v[i].ClusterName]; referenced {
				addResource(targets, v[i], resource.EndpointType, resources)
			}
		}
	}
	return resources
}

// targetNodes expands a config map key into the node IDs whose snapshots get its resources.
func targetNodes(s string, x *Config) []string {
	nodeInfo, err := util.GetNodeInfo(s)
	if err != nil {
		return nil
	}
	if len(nodeInfo.Clusters) == 0 && len(nodeInfo.Nodes) == 0 {
		return []string{util.GetNodeID(map[string]string{"nodes": x.NodeID, "clusters": x.Cluster})}
	}
	var out []string
	for _, c := range nodeInfo.Clusters {
		for _, n := range getNodes(nodeInfo, x) {
			out = append(out, util.GetNodeID(map[string]string{"nodes": n, "clusters": c}))
		}
	}
	return out
}

func addResource(targets []string, res types.Resource, resType string, resources map[string]map[string][]types.Resource) {
	for _, nodeID := range targets {
		if resources[nodeID] == nil {
			resources[nodeID] = make(map[string][]types.Resource)
		}
		resources[nodeID][resType] = append(resources[nodeID][resType], res)
	}
}

func getNodes(nodeInfo util.NodeInfo, x *Config) []string {
	if len(nodeInfo.Nodes) == 0 {
		return []string{x.NodeID}
	}
	return nodeInfo.Nodes
}

type RouteConfig struct {
	RouteConfiguration *routev3.RouteConfiguration
	Route              *envoyxdsv1alpha1.Route
	ListenerNames      []string
	Matcher            *fcm.Matcher
}

type Process struct {
	Type       string
	Processing bool
}

type Config struct {
	DomainConfigs map[string][]*xdstypes.DomainConfig `yaml:"DomainConfigs"`

	ClusterConfigs map[string][]*cluster.Cluster

	EndpointConfigs map[string][]*endpoint.ClusterLoadAssignment

	ListenerConfigs map[string][]*listener.Listener

	RouteConfigs map[string][]*RouteConfig

	SecretConfigs map[string][]*auth.Secret

	Reconciling map[string]Process

	Storage            xdstypes.StorageConfig      `yaml:"Storage"`
	LetsEncryptAccount xdstypes.LetsEncryptAccount `yaml:"LetsEncryptAccount"`
	VaultClient        *vault.Client

	HTTPClient *http.Client

	// K8sClient is the Kubernetes client for storing certificates in secrets
	K8sClient client.Client

	// DefaultNamespace is the default namespace for storing Kubernetes secrets
	DefaultNamespace string

	// If countdown in minutes will be lesser than this value, it will try to renew certificate
	RenewBeforeExpireInMinutes int `yaml:"RenewBeforeExpireInMinutes"`

	// StatusRefreshInterval caps how long a TLSSecret may go without being
	// reconciled. Without it the requeue delay equals the full time until
	// renewal (up to two months), leaving status fields such as
	// daysUntilExpiry and lastReconciled stale for that whole period.
	StatusRefreshInterval time.Duration `yaml:"StatusRefreshInterval"`

	// RetryBaseDelay is the wait before the first retry after a failed
	// certificate operation. Per-resource overrides are read from the
	// envoyxds.io/retry-base-delay annotation or label.
	RetryBaseDelay time.Duration `yaml:"RetryBaseDelay"`

	// RetryMaxDelay caps the exponential backoff between failed certificate
	// operations.
	RetryMaxDelay time.Duration `yaml:"RetryMaxDelay"`

	// RetryMultiplier is the growth factor of the backoff between failures.
	RetryMultiplier float64 `yaml:"RetryMultiplier"`

	RndNumber int

	NodeID      string
	Cluster     string
	RefreshTime time.Duration

	ReconciliationStatus *status.ReconciliationStatus

	Snapshot *cache.Snapshot

	DryRun bool

	LeClient *LetsEncrypt

	// Leader election status
	IsLeader bool
	LeaderID string
	mu       sync.RWMutex

	// configMu protects all config maps (SecretConfigs, ListenerConfigs, etc.)
	// Controllers acquire write lock, snapshot generation acquires read lock
	configMu sync.RWMutex

	// Counter for configuration changes
	configChangeCounter atomic.Uint64

	snapshotMemo snapshotMemo
}

// LockConfig acquires write lock on config maps - use when modifying configs
func (c *Config) LockConfig() {
	c.configMu.Lock()
}

// UnlockConfig releases write lock on config maps
func (c *Config) UnlockConfig() {
	c.configMu.Unlock()
}

// RLockConfig acquires read lock on config maps - use when reading configs
func (c *Config) RLockConfig() {
	c.configMu.RLock()
}

// RUnlockConfig releases read lock on config maps
func (c *Config) RUnlockConfig() {
	c.configMu.RUnlock()
}

// IncrementConfigCounter increments the config change counter
func (c *Config) IncrementConfigCounter() uint64 {
	return c.configChangeCounter.Add(1)
}

// GetConfigCounter returns the current value of the config change counter
func (c *Config) GetConfigCounter() uint64 {
	return c.configChangeCounter.Load()
}

// SetLeaderStatus updates the leader status of this instance
func (c *Config) SetLeaderStatus(isLeader bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.IsLeader = isLeader
	if isLeader {
		c.LeaderID = c.NodeID
	}
}

// IsLeaderInstance returns true if this instance is the leader
func (c *Config) IsLeaderInstance() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.IsLeader
}

// GetLeaderID returns the current leader's node ID
func (c *Config) GetLeaderID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.LeaderID
}

type SnapshotConfig struct {
	NodeID   string
	Version  string
	Snapshot *cache.Snapshot
	// Inconsistent is the Snapshot.Consistent result.
	Inconsistent error
}
