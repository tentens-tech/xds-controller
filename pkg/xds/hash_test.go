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

package xds

import (
	"testing"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

func routeWith(name string, domains ...string) *routev3.RouteConfiguration {
	return &routev3.RouteConfiguration{Name: name, VirtualHosts: []*routev3.VirtualHost{{Name: "vh", Domains: domains}}}
}

func TestGetHash_IgnoresOrder(t *testing.T) {
	a := map[string][]types.Resource{resource.RouteType: {routeWith("a", "x.example", "y.example"), routeWith("b")}}
	b := map[string][]types.Resource{resource.RouteType: {routeWith("b"), routeWith("a", "y.example", "x.example")}}
	assert.Equal(t, GetHash(a), GetHash(b))
}

func TestGetHash_SeesChanges(t *testing.T) {
	base := GetHash(map[string][]types.Resource{resource.RouteType: {routeWith("a", "x.example")}})
	for name, res := range map[string]map[string][]types.Resource{
		"domain":         {resource.RouteType: {routeWith("a", "z.example")}},
		"name":           {resource.RouteType: {routeWith("b", "x.example")}},
		"extra resource": {resource.RouteType: {routeWith("a", "x.example"), routeWith("b")}},
		"type":           {resource.ListenerType: {routeWith("a", "x.example")}},
	} {
		assert.NotEqual(t, base, GetHash(res), name)
	}
}

// Anys built from equal configs can hold different bytes (map order); the version must not change.
func TestGetHash_AnyByContent(t *testing.T) {
	pack := func() *anypb.Any {
		meta, err := structpb.NewStruct(map[string]any{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7, "h": 8})
		require.NoError(t, err)
		a, err := anypb.New(&hcmv3.HttpConnectionManager{
			StatPrefix: "s",
			RouteSpecifier: &hcmv3.HttpConnectionManager_RouteConfig{RouteConfig: &routev3.RouteConfiguration{
				VirtualHosts: []*routev3.VirtualHost{{Name: "v", Metadata: &corev3.Metadata{FilterMetadata: map[string]*structpb.Struct{"m": meta}}}},
			}},
		})
		require.NoError(t, err)
		return a
	}
	listener := func(a *anypb.Any) map[string][]types.Resource {
		return map[string][]types.Resource{resource.ListenerType: {&listenerv3.Listener{Name: "l", FilterChains: []*listenerv3.FilterChain{{
			Filters: []*listenerv3.Filter{{Name: "hcm", ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: a}}},
		}}}}}
	}
	want := GetHash(listener(pack()))
	for range 20 {
		require.Equal(t, want, GetHash(listener(pack())))
	}

	changed := pack()
	h := &hcmv3.HttpConnectionManager{}
	require.NoError(t, changed.UnmarshalTo(h))
	h.StatPrefix = "other"
	changed, err := anypb.New(h)
	require.NoError(t, err)
	assert.NotEqual(t, want, GetHash(listener(changed)))
}

func TestHasher_MemoFollowsLiveResources(t *testing.T) {
	var h hasher
	r := routeWith("a", "x.example")
	res := map[string][]types.Resource{resource.RouteType: {r}}
	first := h.resources(res)
	h.endPass()
	assert.Equal(t, first, h.resources(res), "a cached digest must equal a fresh one")
	assert.Equal(t, first, GetHash(res))
	h.endPass()

	res[resource.RouteType] = []types.Resource{routeWith("b")}
	h.resources(res)
	h.endPass()
	h.endPass()
	_, cached := h.prev[r]
	assert.False(t, cached, "a resource no pass reaches is dropped")
}

func TestConsistentMatchesSnapshotConsistent(t *testing.T) {
	eds := &clusterv3.Cluster{Name: "svc", ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_EDS}}
	cases := map[string]map[string][]types.Resource{
		"consistent": {
			resource.ListenerType: {rdsListener(t, "https", "r")},
			resource.RouteType:    {routeWith("r")},
			resource.ClusterType:  {eds},
			resource.EndpointType: {&endpointv3.ClusterLoadAssignment{ClusterName: "svc"}},
		},
		"route missing":    {resource.ListenerType: {rdsListener(t, "https", "r")}},
		"endpoint missing": {resource.ClusterType: {eds}},
		"endpoint extra":   {resource.EndpointType: {&endpointv3.ClusterLoadAssignment{ClusterName: "svc"}}},
		"empty":            {},
	}
	for name, res := range cases {
		snap, err := cache.NewSnapshot("v", res)
		require.NoError(t, err)
		want := snap.Consistent()
		got := consistent(snap, routeReferences(res[resource.ListenerType], nil))
		assert.Equal(t, want == nil, got == nil, "%s: upstream %v, ours %v", name, want, got)
	}
}

func TestRouteReferences_MemoByAny(t *testing.T) {
	var memo passMemo[[]string]
	l := rdsListener(t, "https", "r")
	assert.Equal(t, map[string]bool{"r": true}, routeReferences([]types.Resource{l}, &memo))
	rebuilt, ok := proto.Clone(l).(*listenerv3.Listener)
	require.True(t, ok)
	rebuilt.FilterChains[0].Filters[0].ConfigType = l.FilterChains[0].Filters[0].ConfigType
	assert.Equal(t, map[string]bool{"r": true}, routeReferences([]types.Resource{rebuilt}, &memo))
	assert.Len(t, memo.cur, 1, "a shared HTTP connection manager is decoded once")
}

func BenchmarkGetHash(b *testing.B) {
	res := map[string][]types.Resource{}
	for i := range 500 {
		res[resource.ListenerType] = append(res[resource.ListenerType], rdsListener(b, "l", "r"))
		res[resource.RouteType] = append(res[resource.RouteType], routeWith("r", "a.example", "b.example", string(rune('a'+i%26))))
	}
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = GetHash(res)
		}
	})
	b.Run("memo", func(b *testing.B) {
		var h hasher
		b.ReportAllocs()
		for b.Loop() {
			_ = h.resources(res)
			h.endPass()
		}
	})
}
