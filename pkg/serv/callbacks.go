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

// Package serv provides the xDS gRPC server implementation.
// It handles Envoy xDS protocol communication and stream management.
package serv

import (
	"context"
	"fmt"
	"sync"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds"
)

type Callbacks struct {
	Signal         chan struct{}
	Debug          bool
	Fetches        int
	Requests       int
	DeltaRequests  int
	DeltaResponses int
	mu             sync.Mutex
}

func (cb *Callbacks) Report() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	log.Log.V(3).Info("server callbacks", "fetches", cb.Fetches, "requests", cb.Requests)
}
func (cb *Callbacks) OnStreamOpen(_ context.Context, id int64, typ string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.Debug {
		log.Log.V(3).Info("stream open", "streamId", id, "streamType", typ)
	}
	if xds.Streams == nil {
		xds.Streams = make(map[int]xds.Stream)
	}
	xds.Streams[int(id)] = xds.Stream{}

	return nil
}

func (cb *Callbacks) OnStreamClosed(id int64, node *corev3.Node) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.Debug {
		log.Log.V(3).Info("stream closed", "streamId", id)
	}

	stream, exists := xds.Streams[int(id)]
	if exists {
		xds.EnvoyStreamActive.Delete(prometheus.Labels{
			"id":      fmt.Sprintf("%d", id),
			"cluster": stream.Cluster,
			"node":    stream.Node,
			"version": stream.EnvoyVersion,
		})
		delete(xds.Streams, int(id))
	}
}

func (cb *Callbacks) OnDeltaStreamOpen(_ context.Context, id int64, typ string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.Debug {
		log.Log.V(3).Info("delta stream open", "streamId", id, "streamType", typ)
	}
	return nil
}

func (cb *Callbacks) OnDeltaStreamClosed(id int64, node *corev3.Node) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.Debug {
		log.Log.V(3).Info("delta stream closed", "streamId", id)
	}
}

func (cb *Callbacks) OnStreamRequest(i int64, r *discovery.DiscoveryRequest) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.Requests++
	if cb.Signal != nil {
		close(cb.Signal)
		cb.Signal = nil
	}

	if cb.Debug {
		log.Log.V(3).Info("stream request", "Cluster", r.Node.Cluster, "Node", r.Node.Id, "Metadata", r.Node.Metadata)
	}

	xds.RecordEnvoyStreamActive(fmt.Sprintf("%d", i), r.Node.Cluster, r.Node.Id, getVersion(r.Node))
	nodeID := util.GetNodeID(map[string]string{"clusters": r.Node.Cluster, "nodes": r.Node.Id})
	r.Node.Id = nodeID
	return nil
}

func (cb *Callbacks) OnStreamResponse(_ context.Context, i int64, req *discovery.DiscoveryRequest, rsp *discovery.DiscoveryResponse) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	nodeInfo, err := util.GetNodeInfo(req.Node.Id)
	if err != nil {
		log.Log.Error(err, "failed to get node info", "nodeId", req.Node.Id)
		return
	}
	req.Node.Id = nodeInfo.Nodes[0]

	xds.Streams[int(i)] = xds.Stream{
		Node:         nodeInfo.Nodes[0],
		Cluster:      nodeInfo.Clusters[0],
		EnvoyVersion: getVersion(req.Node),
	}

	for _, s := range xds.GetSnapshots() {
		xds.SetSnapshotServerVersion(s.Node, s.Cluster, s.SnapshotVersion)
	}
	xds.SetSnapshotClientVersion(nodeInfo.Nodes[0], nodeInfo.Clusters[0], rsp.GetVersionInfo())

	if cb.Debug {
		log.Log.V(3).Info("stream response", "Cluster", req.Node.Cluster, "Node", req.Node.Id, "Metadata", req.Node.Metadata)
	}

	for _, stream := range xds.Streams {
		if stream.SnapshotClientVersion != "" && stream.SnapshotServerVersion != "" {
			xds.RecordSnapshotVersionMatchSet(stream.Cluster, stream.Node)
		}
	}
}

func (cb *Callbacks) OnStreamDeltaResponse(id int64, req *discovery.DeltaDiscoveryRequest, res *discovery.DeltaDiscoveryResponse) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.DeltaResponses++
}

func (cb *Callbacks) OnStreamDeltaRequest(id int64, req *discovery.DeltaDiscoveryRequest) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.DeltaRequests++
	if cb.Signal != nil {
		close(cb.Signal)
		cb.Signal = nil
	}
	if cb.Debug {
		log.Log.V(3).Info("delta stream request", "Cluster", req.Node.Cluster, "Node", req.Node.Id, "Metadata", req.Node.Metadata)
	}
	nodeID := util.GetNodeID(map[string]string{"clusters": req.Node.Cluster, "nodes": req.Node.Id})
	req.Node.Id = nodeID
	return nil
}

func (cb *Callbacks) OnFetchRequest(_ context.Context, req *discovery.DiscoveryRequest) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.Fetches++
	if cb.Signal != nil {
		close(cb.Signal)
		cb.Signal = nil
	}
	if cb.Debug {
		log.Log.V(3).Info("fetch  stream request", "Cluster", req.Node.Cluster, "Node", req.Node.Id, "Metadata", req.Node.Metadata)
	}
	return nil
}
func (cb *Callbacks) OnFetchResponse(*discovery.DiscoveryRequest, *discovery.DiscoveryResponse) {}

func getVersion(n *corev3.Node) string {
	ver := n.GetUserAgentBuildVersion().GetVersion()
	return fmt.Sprintf("v%d.%d.%d", ver.MajorNumber, ver.MinorNumber, ver.Patch)
}
