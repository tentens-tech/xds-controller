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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
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
	"google.golang.org/protobuf/encoding/protojson"
	"sigs.k8s.io/controller-runtime/pkg/client"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/status"
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

	// Generate snapshots
	for nodeID, nodeResources := range resources {
		version := GetHash(nodeResources)
		snap, err := cache.NewSnapshot(version, nodeResources)
		if err != nil {
			return nil, fmt.Errorf("failed to create snapshot for node %s: %w", nodeID, err)
		}
		sc = append(sc, SnapshotConfig{
			NodeID:   nodeID,
			Version:  version,
			Snapshot: snap,
		})
	}

	return sc, nil
}

func getResourcesFromSecretConfigs(x *Config, resources map[string]map[string][]types.Resource) map[string]map[string][]types.Resource {
	for s, v := range x.SecretConfigs {
		for i := range v {
			updateResources(s, v[i], resource.SecretType, x, resources)
		}
	}
	return resources
}

func getResourcesFromListenerConfigs(x *Config, resources map[string]map[string][]types.Resource) map[string]map[string][]types.Resource {
	for s, v := range x.ListenerConfigs {
		for i := range v {
			updateResources(s, v[i], resource.ListenerType, x, resources)
		}
	}
	return resources
}

func getResourcesFromClusterConfigs(x *Config, resources map[string]map[string][]types.Resource) map[string]map[string][]types.Resource {
	for s, v := range x.ClusterConfigs {
		for i := range v {
			updateResources(s, v[i], resource.ClusterType, x, resources)
		}
	}
	return resources
}

func getResourcesFromRouteConfigs(x *Config, resources map[string]map[string][]types.Resource) map[string]map[string][]types.Resource {
	for s, v := range x.RouteConfigs {
		for i := range v {
			updateResources(s, v[i].RouteConfiguration, resource.RouteType, x, resources)
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
		for i := range v {
			if _, referenced := edsEndpointNames[v[i].ClusterName]; referenced {
				updateResources(s, v[i], resource.EndpointType, x, resources)
			}
		}
	}
	return resources
}

func updateResources(s string, res types.Resource, resType string, x *Config, resources map[string]map[string][]types.Resource) {
	nodeInfo, err := util.GetNodeInfo(s)
	if err != nil {
		return
	}
	updateFunc := func(nodeID string) {
		if resources[nodeID] == nil {
			resources[nodeID] = make(map[string][]types.Resource)
		}
		resources[nodeID][resType] = append(resources[nodeID][resType], res)
	}

	if len(nodeInfo.Clusters) == 0 && len(nodeInfo.Nodes) == 0 {
		updateFunc(util.GetNodeID(map[string]string{"nodes": x.NodeID, "clusters": x.Cluster}))
		return
	}

	for _, c := range nodeInfo.Clusters {
		for _, n := range getNodes(nodeInfo, x) {
			updateFunc(util.GetNodeID(map[string]string{"nodes": n, "clusters": c}))
		}
	}
}

func getNodes(nodeInfo util.NodeInfo, x *Config) []string {
	if len(nodeInfo.Nodes) == 0 {
		return []string{x.NodeID}
	}
	return nodeInfo.Nodes
}

// sortMapRecursively sorts all arrays and map keys recursively in the structure
func sortMapRecursively(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		// Sort map keys
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		sorted := make(map[string]interface{}, len(v))
		for _, k := range keys {
			sorted[k] = sortMapRecursively(v[k])
		}
		return sorted

	case []interface{}:
		if len(v) == 0 {
			return v
		}

		// Sort slice elements
		sorted := make([]interface{}, len(v))
		for i, val := range v {
			sorted[i] = sortMapRecursively(val)
		}

		// Convert to comparable strings for stable sorting
		strVals := make([]string, len(sorted))
		for i, val := range sorted {
			b, err := json.Marshal(val)
			if err != nil {
				panic(err)
			}
			strVals[i] = string(b)
		}

		// Create index mapping for stable sort
		indices := make([]int, len(strVals))
		for i := range indices {
			indices[i] = i
		}

		// Sort indices based on string values
		sort.SliceStable(indices, func(i, j int) bool {
			return strVals[indices[i]] < strVals[indices[j]]
		})

		// Reorder elements based on sorted indices
		result := make([]interface{}, len(sorted))
		for newIndex, oldIndex := range indices {
			result[newIndex] = sorted[oldIndex]
		}
		return result

	default:
		return v
	}
}

func GetHash(resources map[string][]types.Resource) string {
	// Create single marshaler instance
	marshaler := protojson.MarshalOptions{
		UseProtoNames:  true,
		UseEnumNumbers: true,
		AllowPartial:   true,
	}

	// Convert and sort resources
	resourcesMap := make(map[string][]interface{})
	keys := make([]string, 0, len(resources))
	for k := range resources {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		resourcesSlice := resources[key]
		jsonResources := make([]interface{}, 0, len(resourcesSlice))

		for _, resource := range resourcesSlice {
			// Marshal to JSON bytes
			jsonBytes, err := marshaler.Marshal(resource)
			if err != nil {
				panic(err)
			}

			// Unmarshal to map for sorting
			var jsonMap map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &jsonMap); err != nil {
				panic(err)
			}

			jsonResources = append(jsonResources, jsonMap)
		}

		// Sort the resources
		sortedResources, ok := sortMapRecursively(jsonResources).([]interface{})
		if !ok {
			panic(fmt.Sprintf("unexpected type from sortMapRecursively: %T", sortMapRecursively(jsonResources)))
		}
		resourcesMap[key] = sortedResources
	}

	// Sort the entire structure
	finalMap := sortMapRecursively(resourcesMap)

	// Generate final JSON
	jsonBytes, err := json.Marshal(finalMap)
	if err != nil {
		panic(err)
	}

	// Calculate hash
	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:])
}

type RouteConfig struct {
	RouteConfiguration *routev3.RouteConfiguration
	Route              *envoyxdsv1alpha1.Route
	ListenerNames      []string
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
}
