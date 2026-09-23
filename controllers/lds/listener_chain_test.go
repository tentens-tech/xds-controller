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

package lds

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	hcmtypes "github.com/tentens-tech/xds-controller/pkg/xds/types/hcm"
	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
	routetypes "github.com/tentens-tech/xds-controller/pkg/xds/types/route"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func u32(v uint32) *uint32 { return &v }

var lbRanges = []*lds.CidrRange{
	{AddressPrefix: "35.191.0.0", PrefixLen: u32(16)},
	{AddressPrefix: "130.211.0.0", PrefixLen: u32(22)},
}

func chainRoute(name string, created time.Time, m *lds.FilterChainMatch) *envoyxdsv1alpha1.Route {
	return &envoyxdsv1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(created)},
		Spec: envoyxdsv1alpha1.RouteSpec{Route: routetypes.Route{
			ListenerRefs:     []string{"https"},
			FilterChainMatch: m,
		}},
	}
}

func TestProcessRoutes_SkipsChainsEnvoyWouldReject(t *testing.T) {
	probe := &lds.FilterChainMatch{SourcePrefixRanges: lbRanges}
	cdn := &lds.FilterChainMatch{SourcePrefixRanges: lbRanges, ApplicationProtocols: []string{"h2", "http/1.1"}}

	tests := []struct {
		name   string
		static []*listener.FilterChain
		routes []*envoyxdsv1alpha1.Route
		want   int
	}{
		{"two unmatched chains", nil, []*envoyxdsv1alpha1.Route{chainRoute("a", t0, nil), chainRoute("b", t0, nil)}, 1},
		{"nil and empty matcher", nil, []*envoyxdsv1alpha1.Route{chainRoute("a", t0, nil), chainRoute("b", t0, &lds.FilterChainMatch{})}, 1},
		{"identical source ranges", nil, []*envoyxdsv1alpha1.Route{chainRoute("a", t0, probe), chainRoute("b", t0, probe)}, 1},
		{"health probe next to cdn chain", nil, []*envoyxdsv1alpha1.Route{chainRoute("cdn", t0, cdn), chainRoute("probe", t0, probe)}, 2},
		{"same server name", nil, []*envoyxdsv1alpha1.Route{
			chainRoute("a", t0, &lds.FilterChainMatch{ServerNames: []string{"shop.example.com"}}),
			chainRoute("b", t0, &lds.FilterChainMatch{ServerNames: []string{"Shop.example.com"}}),
		}, 1},
		{"wildcard next to exact name", nil, []*envoyxdsv1alpha1.Route{
			chainRoute("a", t0, &lds.FilterChainMatch{ServerNames: []string{"*.example.com"}}),
			chainRoute("b", t0, &lds.FilterChainMatch{ServerNames: []string{"api.example.com"}}),
		}, 2},
		{"route duplicating a static chain", []*listener.FilterChain{{}}, []*envoyxdsv1alpha1.Route{chainRoute("a", t0, nil)}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := processRoutes(tt.routes, &listener.Listener{FilterChains: tt.static}, "node", nil)
			require.NoError(t, err)
			assert.Len(t, l.FilterChains, tt.want)
		})
	}
}

func TestProcessRoutes_InvalidStaticChain(t *testing.T) {
	static := []*listener.FilterChain{{
		Name:             "bad",
		FilterChainMatch: &listener.FilterChainMatch{SourcePrefixRanges: []*corev3.CidrRange{{AddressPrefix: "not-an-ip"}}},
	}}
	_, err := processRoutes(nil, &listener.Listener{FilterChains: static}, "node", nil)
	require.ErrorContains(t, err, `filter chain "bad"`)
}

func testListenerReconciler() *ListenerReconciler {
	return &ListenerReconciler{Config: &xds.Config{
		NodeID:               "global",
		Cluster:              "global",
		RouteConfigs:         make(map[string][]*xds.RouteConfig),
		ListenerConfigs:      make(map[string][]*listener.Listener),
		ReconciliationStatus: status.NewReconciliationStatus(),
	}}
}

func TestGetRoutesForNode_OldestFirst(t *testing.T) {
	r := testListenerReconciler()
	node := util.GetNodeID(map[string]string{"clusters": "apps", "nodes": "01"})
	for _, rt := range []*envoyxdsv1alpha1.Route{
		chainRoute("c", t0.Add(2*time.Hour), nil),
		chainRoute("b", t0, nil),
		chainRoute("a", t0, nil),
		chainRoute("d", t0.Add(time.Hour), nil),
	} {
		r.Config.RouteConfigs[node] = append(r.Config.RouteConfigs[node], &xds.RouteConfig{Route: rt, ListenerNames: rt.Spec.ListenerRefs})
	}

	routes := r.getRoutesForNode(envoyxdsv1alpha1.Listener{ObjectMeta: metav1.ObjectMeta{Name: "https"}}, node)
	names := make([]string, 0, len(routes))
	for _, rt := range routes {
		names = append(names, rt.Name)
	}
	assert.Equal(t, []string{"a", "b", "d", "c"}, names)
	assert.True(t, r.hasRoutesFor("https", []string{node}))
	assert.False(t, r.hasRoutesFor("quic", []string{node}))
}

// Run with -race: LDS reads must hold the config lock while RDS writes.
func TestListenerReadsWhileRoutesChange(t *testing.T) {
	r := testListenerReconciler()
	node := util.GetNodeID(map[string]string{"clusters": "apps", "nodes": "01"})
	l := envoyxdsv1alpha1.Listener{ObjectMeta: metav1.ObjectMeta{Name: "https"}}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 500 {
			rt := chainRoute(fmt.Sprintf("r%d", i), t0, nil)
			r.Config.LockConfig()
			r.Config.RouteConfigs[node] = append(r.Config.RouteConfigs[node], &xds.RouteConfig{Route: rt, ListenerNames: rt.Spec.ListenerRefs})
			if i%3 == 0 {
				delete(r.Config.RouteConfigs, node)
			}
			r.Config.UnlockConfig()
		}
	}()
	for range 500 {
		_ = r.getRoutesForNode(l, node)
		_ = r.hasRoutesFor("https", []string{node})
	}
	wg.Wait()
}

func BenchmarkProcessRoutes(b *testing.B) {
	for _, n := range []int{20, 100} {
		routes := make([]*envoyxdsv1alpha1.Route, 0, n)
		for i := range n {
			routes = append(routes, chainRoute(fmt.Sprintf("r%d", i), t0, &lds.FilterChainMatch{ServerNames: []string{fmt.Sprintf("host-%d.example.com", i)}}))
		}
		b.Run(fmt.Sprintf("routes=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := processRoutes(routes, &listener.Listener{}, "node", nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestProcessRoutes_DuplicateStaticChains(t *testing.T) {
	static := []*listener.FilterChain{{Name: "a"}, {Name: "b"}}
	_, err := processRoutes(nil, &listener.Listener{FilterChains: static}, "node", nil)
	require.ErrorContains(t, err, `filter chain "b"`)
}

func TestProcessRoutes_BadRouteDoesNotBlockListener(t *testing.T) {
	bad := chainRoute("bad", t0, &lds.FilterChainMatch{SourceType: "external"})
	good := chainRoute("good", t0, &lds.FilterChainMatch{ServerNames: []string{"shop.example.com"}})
	l, err := processRoutes([]*envoyxdsv1alpha1.Route{bad, good}, &listener.Listener{Name: "https"}, "node", nil)
	require.NoError(t, err)
	assert.Len(t, l.FilterChains, 1)
}

func newListenerHarness(t *testing.T, objs ...client.Object) *ListenerReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))
	r := testListenerReconciler()
	r.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(&envoyxdsv1alpha1.Listener{}).Build()
	r.Scheme = scheme
	return r
}

func TestReconcile_WaitsForRoutesAndClearsPending(t *testing.T) {
	l := &envoyxdsv1alpha1.Listener{ObjectMeta: metav1.ObjectMeta{Name: "https", Namespace: "xds-system"}}
	r := newListenerHarness(t, l)
	rs := r.Config.ReconciliationStatus
	node := util.GetNodeID(map[string]string{"clusters": "global", "nodes": "global"})
	rt := chainRoute("r", t0, &lds.FilterChainMatch{ServerNames: []string{"shop.example.com"}})
	r.Config.RouteConfigs[node] = []*xds.RouteConfig{{Route: rt, ListenerNames: rt.Spec.ListenerRefs}}
	rs.MarkListenerPending("xds-system/https")
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "xds-system", Name: "https"}}

	res, err := r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Positive(t, res.RequeueAfter, "LDS must wait until RDS has listed its routes")
	assert.Empty(t, r.Config.ListenerConfigs[node])
	assert.True(t, rs.HasPendingListeners())

	rs.SetRoutesInitialized(true)
	_, err = r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, r.Config.ListenerConfigs[node], 1)
	assert.Len(t, r.Config.ListenerConfigs[node][0].FilterChains, 1)
	assert.False(t, rs.HasPendingListeners())
}

func TestReconcile_KeepsRouteConfigsWhenListenerDeleted(t *testing.T) {
	r := newListenerHarness(t)
	r.Config.ReconciliationStatus.SetRoutesInitialized(true)
	node := util.GetNodeID(map[string]string{"clusters": "global", "nodes": "global"})
	rt := chainRoute("r", t0, nil)
	r.Config.RouteConfigs[node] = []*xds.RouteConfig{{Route: rt, ListenerNames: rt.Spec.ListenerRefs}}
	r.Config.ListenerConfigs[node] = []*listener.Listener{{Name: "https"}}

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "xds-system", Name: "https"}})
	require.NoError(t, err)
	assert.Empty(t, r.Config.ListenerConfigs[node])
	assert.Len(t, r.Config.RouteConfigs[node], 1, "route placement belongs to RDS")
}

func TestReconcile_FailedGetKeepsListenerPending(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))
	r := testListenerReconciler()
	r.Scheme = scheme
	r.Client = fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return errors.New("apiserver unavailable")
		},
	}).Build()
	rs := r.Config.ReconciliationStatus
	rs.SetRoutesInitialized(true)
	rs.MarkListenerPending("xds-system/https")

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "xds-system", Name: "https"}})
	require.Error(t, err)
	assert.True(t, rs.HasPendingListeners(), "a listener that was not rebuilt must stay pending")
}

func TestChainCache_ReusesChainsPerRouteVersion(t *testing.T) {
	var c chainCache
	shop := chainRoute("shop", t0, &lds.FilterChainMatch{ServerNames: []string{"shop.example.com"}})
	build := func(routes ...*envoyxdsv1alpha1.Route) *listener.Listener {
		l, err := processRoutes(routes, &listener.Listener{Name: "https"}, "node", &c)
		require.NoError(t, err)
		return l
	}

	first := build(shop)
	assert.Same(t, first.FilterChains[0], build(shop).FilterChains[0], "an unchanged route reuses its chain")

	edited := shop.DeepCopy()
	edited.Spec.FilterChainMatch.ServerNames = []string{"store.example.com"}
	rebuilt := build(edited)
	assert.NotSame(t, first.FilterChains[0], rebuilt.FilterChains[0], "a new route version builds a new chain")
	assert.Equal(t, []string{"store.example.com"}, rebuilt.FilterChains[0].GetFilterChainMatch().GetServerNames())

	c.prune(map[*envoyxdsv1alpha1.Route]struct{}{edited: {}})
	assert.Len(t, c.entries, 1, "chains of replaced routes are dropped")
}

func TestPrepareFilters_LeavesRouteUntouched(t *testing.T) {
	r := chainRoute("shop", t0, nil)
	r.Spec.Rds = &hcmtypes.Rds{}
	before := r.DeepCopy()

	filters, err := prepareFilters(r, true)
	require.NoError(t, err)
	assert.Equal(t, before, r)

	h := &hcm.HttpConnectionManager{}
	require.NoError(t, filters[0].GetTypedConfig().UnmarshalTo(h))
	assert.Equal(t, "shop", h.GetRds().GetRouteConfigName())
	assert.NotNil(t, h.GetRds().GetConfigSource().GetAds())
	assert.Equal(t, hcm.HttpConnectionManager_HTTP3, h.GetCodecType())
}
