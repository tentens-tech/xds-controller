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

package serv

import (
	"context"
	"fmt"
	"net"
	"time"

	clusterservice "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	listenerservice "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	routeservice "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"
	runtimeservice "github.com/envoyproxy/go-control-plane/envoy/service/runtime/v3"
	secretservice "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds"
)

// gRPC server configuration constants.
const (
	grpcKeepaliveTime        = 30 * time.Second
	grpcKeepaliveTimeout     = 5 * time.Second
	grpcKeepaliveMinTime     = 30 * time.Second
	grpcMaxConcurrentStreams = 1000000

	// Interval for checking configuration changes and reconciliation status.
	snapshotCheckInterval = 5 * time.Second
)

// snapVersions tracks the last known version for each node's snapshot.
var snapVersions = make(map[string]string)

// ServerConfig holds the configuration for the xDS server.
type ServerConfig struct {
	Cache     cache.SnapshotCache
	Srv       server.Server
	Callbacks *Callbacks
	Port      uint
	Config    *xds.Config
}

// ServerInit creates a new ServerConfig with the given port and cache.
func ServerInit(port uint, c cache.SnapshotCache) *ServerConfig {
	return &ServerConfig{
		Cache: c,
		Callbacks: &Callbacks{
			Signal: make(chan struct{}),
			Debug:  true,
		},
		Port: port,
	}
}

// Start implements the controller-runtime Runnable interface.
// It starts the xDS gRPC server and processes snapshot updates.
func (s *ServerConfig) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)

	// Create xDS server
	srv := server.NewServer(ctx, s.Cache, s.Callbacks)

	// Configure gRPC server with keepalive settings
	// These settings prevent availability problems when proxies multiplex
	// requests over a single connection to the management server.
	grpcServer := grpc.NewServer(
		grpc.MaxConcurrentStreams(grpcMaxConcurrentStreams),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    grpcKeepaliveTime,
			Timeout: grpcKeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             grpcKeepaliveMinTime,
			PermitWithoutStream: true,
		}),
	)

	// Register all xDS services
	registerServer(grpcServer, srv)

	// Start listening
	addr := fmt.Sprintf(":%d", s.Port)
	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	logger.V(2).Info("Starting xDS server",
		"addr", addr,
		"rndNumber", s.Config.RndNumber,
	)

	// Start gRPC server in background
	errCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			errCh <- fmt.Errorf("gRPC server error: %w", err)
		}
	}()

	// Track the last processed configuration counter
	lastProcessedCounter := s.Config.GetConfigCounter()

	// Use ticker for efficient polling instead of busy-wait
	ticker := time.NewTicker(snapshotCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.V(0).Info("Shutting down xDS server")
			grpcServer.GracefulStop()
			return nil

		case err := <-errCh:
			return err

		case <-ticker.C:
			// Skip processing if not all controllers have reconciled
			if !s.Config.ReconciliationStatus.AllReconciled() {
				continue
			}

			// Skip if no configuration changes since last processing
			currentCounter := s.Config.GetConfigCounter()
			if currentCounter == lastProcessedCounter {
				continue
			}

			// Process snapshot update
			if err := processSnapshot(ctx, s); err != nil {
				logger.Error(err, "Failed to process snapshot")
				xds.RecordConfigError("Snapshot", "XDS", "unable to process snapshot: "+err.Error())
				continue
			}

			lastProcessedCounter = currentCounter
		}
	}
}

// registerServer registers all xDS discovery services with the gRPC server.
func registerServer(grpcServer *grpc.Server, srv server.Server) {
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)
	endpointservice.RegisterEndpointDiscoveryServiceServer(grpcServer, srv)
	clusterservice.RegisterClusterDiscoveryServiceServer(grpcServer, srv)
	routeservice.RegisterRouteDiscoveryServiceServer(grpcServer, srv)
	listenerservice.RegisterListenerDiscoveryServiceServer(grpcServer, srv)
	secretservice.RegisterSecretDiscoveryServiceServer(grpcServer, srv)
	runtimeservice.RegisterRuntimeDiscoveryServiceServer(grpcServer, srv)
}

// isSnapToUpdate checks if a snapshot needs to be updated based on version changes.
func isSnapToUpdate(snap xds.SnapshotConfig) bool {
	currentVersion, exists := snapVersions[snap.NodeID]
	if !exists || currentVersion != snap.Version {
		snapVersions[snap.NodeID] = snap.Version
		return true
	}
	return false
}

// processSnapshot generates and applies snapshot updates for all nodes.
func processSnapshot(ctx context.Context, s *ServerConfig) error {
	logger := log.FromContext(ctx)

	// Generate snapshots for all nodes
	snapshots, err := xds.GenerateSnapshotsV2(ctx, s.Config)
	if err != nil {
		return fmt.Errorf("failed to generate snapshots: %w", err)
	}

	// Clean metrics for snapshot version tracking
	xds.CleanSnapshots()

	for _, conf := range snapshots {
		nodeInfo, err := util.GetNodeInfo(conf.NodeID)
		if err != nil {
			logger.Error(err, "failed to get node info", "nodeId", conf.NodeID)
			continue
		}

		// Validate snapshot consistency
		if err := conf.Snapshot.Consistent(); err != nil {
			logger.V(1).Error(err, "Snapshot inconsistency detected",
				"version", conf.Version,
				"cluster", nodeInfo.Clusters[0],
				"node", nodeInfo.Nodes[0],
				"LDS", len(conf.Snapshot.GetResources(resource.ListenerType)),
				"RDS", len(conf.Snapshot.GetResources(resource.RouteType)),
				"CDS", len(conf.Snapshot.GetResources(resource.ClusterType)),
				"EDS", len(conf.Snapshot.GetResources(resource.EndpointType)),
				"SDS", len(conf.Snapshot.GetResources(resource.SecretType)),
			)
			s.Cache.ClearSnapshot(conf.NodeID)
			return fmt.Errorf("snapshot inconsistency for node %s: %w", conf.NodeID, err)
		}

		// Update cache if snapshot version changed
		if isSnapToUpdate(conf) {
			logger.V(0).Info("Upgrading Envoy snapshot",
				"version", conf.Version,
				"cluster", nodeInfo.Clusters[0],
				"node", nodeInfo.Nodes[0],
				"LDS", len(conf.Snapshot.GetResources(resource.ListenerType)),
				"RDS", len(conf.Snapshot.GetResources(resource.RouteType)),
				"CDS", len(conf.Snapshot.GetResources(resource.ClusterType)),
				"EDS", len(conf.Snapshot.GetResources(resource.EndpointType)),
				"SDS", len(conf.Snapshot.GetResources(resource.SecretType)),
			)

			// Record metrics
			xds.RecordSnapshotUpdateCount(nodeInfo.Clusters[0], nodeInfo.Nodes[0])
			xds.RecordResourceCount("LDS", nodeInfo.Clusters[0], nodeInfo.Nodes[0], float64(len(conf.Snapshot.GetResources(resource.ListenerType))))
			xds.RecordResourceCount("CDS", nodeInfo.Clusters[0], nodeInfo.Nodes[0], float64(len(conf.Snapshot.GetResources(resource.ClusterType))))
			xds.RecordResourceCount("RDS", nodeInfo.Clusters[0], nodeInfo.Nodes[0], float64(len(conf.Snapshot.GetResources(resource.RouteType))))
			xds.RecordResourceCount("EDS", nodeInfo.Clusters[0], nodeInfo.Nodes[0], float64(len(conf.Snapshot.GetResources(resource.EndpointType))))
			xds.RecordResourceCount("SDS", nodeInfo.Clusters[0], nodeInfo.Nodes[0], float64(len(conf.Snapshot.GetResources(resource.SecretType))))

			// Apply snapshot to cache
			if err := s.Cache.SetSnapshot(ctx, conf.NodeID, conf.Snapshot); err != nil {
				return fmt.Errorf("failed to set snapshot for node %s: %w", conf.NodeID, err)
			}
		}

		s.Config.ReconciliationStatus.SetSnapshotGenerated(true)

		// Add snapshot to metrics system
		xds.AddSnapshot(nodeInfo.Nodes[0], nodeInfo.Clusters[0], conf.Version)
	}

	// Record version match metrics for active streams
	for _, stream := range xds.Streams {
		if stream.SnapshotClientVersion != "" && stream.SnapshotServerVersion != "" {
			xds.RecordSnapshotVersionMatchSet(stream.Cluster, stream.Node)
		}
	}

	return nil
}

// SleepWithContext sleeps for the specified duration or until the context is canceled.
func SleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
