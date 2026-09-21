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

package rds

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	"github.com/tentens-tech/xds-controller/pkg/xds/fcm"
	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
	rdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/rds"
	routetypes "github.com/tentens-tech/xds-controller/pkg/xds/types/route"
)

const testNS = "xds-system"

var (
	t0       = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	lbRanges = []*lds.CidrRange{cidr("35.191.0.0", 16), cidr("130.211.0.0", 22)}
)

func u32(v uint32) *uint32 { return &v }

func cidr(addr string, bits uint32) *lds.CidrRange {
	return &lds.CidrRange{AddressPrefix: addr, PrefixLen: u32(bits)}
}

func sni(names ...string) *lds.FilterChainMatch { return &lds.FilterChainMatch{ServerNames: names} }

func testListener(name, clusters, nodes string) *envoyxdsv1alpha1.Listener {
	return &envoyxdsv1alpha1.Listener{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: testNS,
		Annotations: map[string]string{"clusters": clusters, "nodes": nodes},
	}}
}

type routeOpt func(*envoyxdsv1alpha1.Route)

func onListeners(names ...string) routeOpt {
	return func(r *envoyxdsv1alpha1.Route) { r.Spec.ListenerRefs = names }
}

func inCluster(clusters, nodes string) routeOpt {
	return func(r *envoyxdsv1alpha1.Route) {
		r.Annotations = map[string]string{"clusters": clusters, "nodes": nodes}
	}
}

func withMatch(m *lds.FilterChainMatch) routeOpt {
	return func(r *envoyxdsv1alpha1.Route) { r.Spec.FilterChainMatch = m }
}

func withDomains(domains ...string) routeOpt {
	return func(r *envoyxdsv1alpha1.Route) {
		r.Spec.RouteConfig.VirtualHosts = []*rdstypes.VirtualHost{{Name: "vh", Domains: domains}}
	}
}

func testRoute(name string, created time.Time, opts ...routeOpt) *envoyxdsv1alpha1.Route {
	r := &envoyxdsv1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: testNS,
			CreationTimestamp: metav1.NewTime(created),
			Annotations:       map[string]string{"clusters": "apps", "nodes": "01"},
		},
		Spec: envoyxdsv1alpha1.RouteSpec{Route: routetypes.Route{
			ListenerRefs: []string{"https"},
			RouteConfig: &rdstypes.RDS{
				Name:         name,
				VirtualHosts: []*rdstypes.VirtualHost{{Name: "vh", Domains: []string{"*"}}},
			},
		}},
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

type harness struct {
	t              *testing.T
	client         client.Client
	r              *RouteReconciler
	listenerEvents chan event.GenericEvent
}

func newHarness(t *testing.T, objs ...client.Object) *harness {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&envoyxdsv1alpha1.Route{}).
		Build()
	events := make(chan event.GenericEvent, 1024)
	return &harness{
		t:      t,
		client: c,
		r: &RouteReconciler{
			Client: c,
			Scheme: scheme,
			Config: &xds.Config{
				NodeID:               "global",
				Cluster:              "global",
				RouteConfigs:         make(map[string][]*xds.RouteConfig),
				ReconciliationStatus: status.NewReconciliationStatus(),
			},
			ListenerEvents: events,
			requeue:        make(chan event.GenericEvent, 1024),
		},
		listenerEvents: events,
	}
}

func (h *harness) reconcile(names ...string) {
	h.t.Helper()
	for _, name := range names {
		_, err := h.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNS, Name: name}})
		require.NoError(h.t, err)
	}
}

// placement returns "cluster/node" for every node the route is served on.
func (h *harness) placement(name string) []string {
	h.r.Config.RLockConfig()
	defer h.r.Config.RUnlockConfig()
	var out []string
	for nodeID, routes := range h.r.Config.RouteConfigs {
		for _, rc := range routes {
			if rc.Route.Name == name {
				info, err := util.GetNodeInfo(nodeID)
				require.NoError(h.t, err)
				out = append(out, strings.Join(info.Clusters, ",")+"/"+strings.Join(info.Nodes, ","))
			}
		}
	}
	slices.Sort(out)
	return out
}

func (h *harness) get(name string) *envoyxdsv1alpha1.Route {
	h.t.Helper()
	var r envoyxdsv1alpha1.Route
	require.NoError(h.t, h.client.Get(context.Background(), types.NamespacedName{Namespace: testNS, Name: name}, &r))
	return &r
}

func (h *harness) update(name string, mutate func(*envoyxdsv1alpha1.Route)) {
	h.t.Helper()
	r := h.get(name)
	mutate(r)
	require.NoError(h.t, h.client.Update(context.Background(), r))
}

func (h *harness) requireActive(name string) {
	h.t.Helper()
	r := h.get(name)
	assert.True(h.t, r.Status.Active, "%s should be active, message %q", name, r.Status.Message)
	assert.NotEmpty(h.t, h.placement(name), "%s should be served", name)
}

func (h *harness) requireRejected(name, msgPart string) {
	h.t.Helper()
	r := h.get(name)
	assert.False(h.t, r.Status.Active, "%s should be inactive", name)
	assert.Contains(h.t, r.Status.Message, msgPart)
	cond := meta.FindStatusCondition(r.Status.Conditions, envoyxdsv1alpha1.RouteConditionError)
	if assert.NotNil(h.t, cond) {
		assert.Equal(h.t, metav1.ConditionTrue, cond.Status)
	}
	assert.Empty(h.t, h.placement(name), "%s must not be served on any node", name)
}

func drainNames(ch chan event.GenericEvent) []string {
	var out []string
	for {
		select {
		case e := <-ch:
			out = append(out, e.Object.GetName())
		default:
			slices.Sort(out)
			return slices.Compact(out)
		}
	}
}

func TestReconcile_SameSNIDifferentClustersCoexist(t *testing.T) {
	h := newHarness(t,
		testListener("https", "edge,apps", "01,02"),
		testRoute("edge-shop", t0, inCluster("edge", "01,02"), withMatch(sni("shop.example.com"))),
		testRoute("apps-shop", t0.Add(time.Hour), inCluster("apps", "01,02"), withMatch(sni("shop.example.com"))),
	)
	h.reconcile("edge-shop", "apps-shop")

	h.requireActive("edge-shop")
	h.requireActive("apps-shop")
	assert.Equal(t, []string{"edge/01", "edge/02"}, h.placement("edge-shop"))
	assert.Equal(t, []string{"apps/01", "apps/02"}, h.placement("apps-shop"))
}

func TestReconcile_SameSNIDifferentListenersCoexist(t *testing.T) {
	h := newHarness(t,
		testListener("https", "apps", "01"),
		testListener("https-alt", "apps", "01"),
		testRoute("a", t0, onListeners("https"), withMatch(sni("shop.example.com"))),
		testRoute("b", t0.Add(time.Hour), onListeners("https-alt"), withMatch(sni("shop.example.com"))),
	)
	h.reconcile("a", "b")

	h.requireActive("a")
	h.requireActive("b")
}

func TestReconcile_LBHealthProbeNextToCDNChain(t *testing.T) {
	h := newHarness(t,
		testListener("https", "apps", "01"),
		testRoute("cdn-chain", t0, withMatch(&lds.FilterChainMatch{
			SourcePrefixRanges: lbRanges, ApplicationProtocols: []string{"h2", "http/1.1"},
		})),
		testRoute("lb-health-probe", t0.Add(time.Hour), withMatch(&lds.FilterChainMatch{SourcePrefixRanges: lbRanges})),
	)
	h.reconcile("cdn-chain", "lb-health-probe")

	h.requireActive("cdn-chain")
	h.requireActive("lb-health-probe")
}

func TestReconcile_NewerDuplicateRejected(t *testing.T) {
	probe := &lds.FilterChainMatch{SourcePrefixRanges: lbRanges}
	h := newHarness(t,
		testListener("https", "apps", "01,02"),
		testRoute("lb-health-probe", t0, inCluster("apps", "01,02"), withMatch(probe)),
		testRoute("lb-health-probe-copy", t0.Add(time.Hour), inCluster("apps", "01,02"), withMatch(probe)),
	)
	h.reconcile("lb-health-probe", "lb-health-probe-copy")

	h.requireActive("lb-health-probe")
	h.requireRejected("lb-health-probe-copy", "older route 'lb-health-probe'")
}

func TestReconcile_UnmatchedChainsOnOneListenerConflict(t *testing.T) {
	h := newHarness(t,
		testListener("grpc-web", "apps", "01"),
		testRoute("grpc-route", t0, onListeners("grpc", "grpc-web")),
		testRoute("grpc-web-route", t0.Add(time.Hour), onListeners("grpc-web")),
	)
	h.reconcile("grpc-route", "grpc-web-route")

	h.requireActive("grpc-route")
	h.requireRejected("grpc-web-route", "same matching rules")
}

func TestReconcile_OlderRouteEvictsNewerFromEveryNode(t *testing.T) {
	probe := &lds.FilterChainMatch{SourcePrefixRanges: lbRanges}
	h := newHarness(t,
		testListener("https", "apps", "01,02"),
		testRoute("newer", t0.Add(time.Hour), inCluster("apps", "01,02"), withMatch(probe)),
	)
	h.reconcile("newer")
	require.Equal(t, []string{"apps/01", "apps/02"}, h.placement("newer"))
	drainNames(h.listenerEvents)

	// The older route only shares node 01, yet the loser leaves node 02 as well.
	require.NoError(t, h.client.Create(context.Background(), testRoute("older", t0, inCluster("apps", "01"), withMatch(probe))))
	h.reconcile("older")

	h.requireActive("older")
	assert.Empty(t, h.placement("newer"))
	assert.Equal(t, []string{"newer"}, drainNames(h.r.requeue))
	assert.Equal(t, []string{"https"}, drainNames(h.listenerEvents))

	h.reconcile("newer")
	h.requireRejected("newer", "older route 'older'")
}

func TestReconcile_LoserReturnsWhenWinnerDeleted(t *testing.T) {
	probe := &lds.FilterChainMatch{SourcePrefixRanges: lbRanges}
	winner := testRoute("winner", t0, withMatch(probe))
	h := newHarness(t,
		testListener("https", "apps", "01"),
		winner,
		testRoute("loser", t0.Add(time.Hour), withMatch(probe)),
	)
	h.reconcile("winner", "loser")
	h.requireRejected("loser", "older route 'winner'")

	require.NoError(t, h.client.Delete(context.Background(), h.get("winner")))
	h.reconcile("winner")
	assert.Equal(t, []string{"loser"}, drainNames(h.r.requeue))

	h.reconcile("loser")
	h.requireActive("loser")
	cond := meta.FindStatusCondition(h.get("loser").Status.Conditions, envoyxdsv1alpha1.RouteConditionError)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
}

func TestReconcile_WildcardShadowNeedsOverlappingVirtualHosts(t *testing.T) {
	t.Run("overlapping virtual hosts conflict", func(t *testing.T) {
		h := newHarness(t,
			testListener("https", "apps", "01"),
			testRoute("wildcard", t0, withMatch(sni("*.example.com"))),
			testRoute("exact", t0.Add(time.Hour), withMatch(sni("api.example.com"))),
		)
		h.reconcile("wildcard", "exact")
		h.requireActive("wildcard")
		h.requireRejected("exact", "wildcard")
	})
	t.Run("separate virtual hosts coexist", func(t *testing.T) {
		h := newHarness(t,
			testListener("https", "apps", "01"),
			testRoute("wildcard", t0, withMatch(sni("*.example.com")), withDomains("*.example.com")),
			testRoute("exact", t0.Add(time.Hour), withMatch(sni("api.example.com")), withDomains("api.example.com")),
		)
		h.reconcile("wildcard", "exact")
		h.requireActive("wildcard")
		h.requireActive("exact")
	})
}

func TestReconcile_SameCreationTimeResolvesByName(t *testing.T) {
	probe := &lds.FilterChainMatch{SourcePrefixRanges: lbRanges}
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		t.Run(strings.Join(order, "-then-"), func(t *testing.T) {
			h := newHarness(t,
				testListener("https", "apps", "01"),
				testRoute("a", t0, withMatch(probe)),
				testRoute("b", t0, withMatch(probe)),
			)
			h.reconcile(order...)
			h.reconcile(drainNames(h.r.requeue)...)

			h.requireActive("a")
			h.requireRejected("b", "older route 'a'")
		})
	}
}

func TestReconcile_InvalidMatcherKeepsLastGoodVersion(t *testing.T) {
	h := newHarness(t,
		testListener("https", "apps", "01"),
		testRoute("r", t0, withMatch(&lds.FilterChainMatch{SourcePrefixRanges: lbRanges})),
	)
	h.reconcile("r")
	h.requireActive("r")

	h.update("r", func(r *envoyxdsv1alpha1.Route) {
		r.Spec.FilterChainMatch = &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("not-an-ip", 8)}}
	})
	h.reconcile("r")

	assert.Contains(t, h.get("r").Status.Message, "invalid filter_chain_match")
	h.r.Config.RLockConfig()
	defer h.r.Config.RUnlockConfig()
	for _, routes := range h.r.Config.RouteConfigs {
		for _, rc := range routes {
			assert.Equal(t, "35.191.0.0", rc.Route.Spec.FilterChainMatch.SourcePrefixRanges[0].AddressPrefix)
		}
	}
}

func TestReconcile_ListenerNotifications(t *testing.T) {
	h := newHarness(t,
		testListener("https", "apps", "01"),
		testListener("quic", "apps", "01"),
		testRoute("r", t0, onListeners("https", "quic"), withMatch(sni("shop.example.com"))),
	)
	h.reconcile("r")
	assert.Equal(t, []string{"https", "quic"}, drainNames(h.listenerEvents))

	h.update("r", func(r *envoyxdsv1alpha1.Route) { r.Spec.ListenerRefs = []string{"https"} })
	h.reconcile("r")
	assert.Equal(t, []string{"https", "quic"}, drainNames(h.listenerEvents), "the dropped listener must be rebuilt too")

	h.update("r", func(r *envoyxdsv1alpha1.Route) { r.Spec.ListenerRefs = nil })
	h.reconcile("r")
	assert.Empty(t, h.placement("r"), "a route without listener_refs must stop being served")
	assert.Equal(t, []string{"https"}, drainNames(h.listenerEvents))

	h.update("r", func(r *envoyxdsv1alpha1.Route) { r.Spec.ListenerRefs = []string{"quic"} })
	h.reconcile("r")
	drainNames(h.listenerEvents)
	require.NoError(t, h.client.Delete(context.Background(), h.get("r")))
	h.reconcile("r")
	assert.Empty(t, h.placement("r"))
	assert.Equal(t, []string{"quic"}, drainNames(h.listenerEvents))
}

func TestRouteConflict(t *testing.T) {
	compile := func(m *lds.FilterChainMatch) *fcm.Matcher {
		c, err := fcm.Compile(m)
		require.NoError(t, err)
		return c
	}
	a := testRoute("a", t0, onListeners("https", "quic"), withMatch(sni("shop.example.com")))
	b := testRoute("b", t0, onListeners("quic"), withMatch(sni("shop.example.com")))
	c := testRoute("c", t0, onListeners("http"), withMatch(sni("shop.example.com")))

	assert.Equal(t, fcm.Duplicate, RouteConflict(a, b, compile(a.Spec.FilterChainMatch), compile(b.Spec.FilterChainMatch)))
	assert.Equal(t, fcm.None, RouteConflict(a, c, compile(a.Spec.FilterChainMatch), compile(c.Spec.FilterChainMatch)))
}

func TestMergeNames(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, mergeNames([]string{"c", "a"}, []string{"b", "a"}))
	assert.Empty(t, mergeNames(nil, nil))
}

func BenchmarkFindConflicts(b *testing.B) {
	for _, perNode := range []int{20, 100, 500} {
		r := &RouteReconciler{Config: &xds.Config{RouteConfigs: make(map[string][]*xds.RouteConfig)}}
		nodes := util.NodeIDs(map[string]string{"clusters": "edge,apps", "nodes": "01,02"}, "", "")
		for _, node := range nodes {
			for i := range perNode {
				rt := testRoute(fmt.Sprintf("route-%d", i), t0, withMatch(sni(fmt.Sprintf("host-%d.example.com", i))))
				m, err := fcm.Compile(rt.Spec.FilterChainMatch)
				require.NoError(b, err)
				r.Config.RouteConfigs[node] = append(r.Config.RouteConfigs[node], &xds.RouteConfig{Route: rt, ListenerNames: rt.Spec.ListenerRefs, Matcher: m})
			}
		}
		current := testRoute("new", t0.Add(time.Hour), withMatch(sni("new.example.com")))
		cm, err := fcm.Compile(current.Spec.FilterChainMatch)
		require.NoError(b, err)

		all := make(map[string]struct{})
		for _, n := range nodes {
			all[n] = struct{}{}
		}
		scoped := map[string]struct{}{nodes[2]: {}, nodes[3]: {}}

		b.Run(fmt.Sprintf("routes=%d/all-4-nodes", perNode), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = r.findConflicts(current, cm, all)
			}
		})
		b.Run(fmt.Sprintf("routes=%d/scoped-2-nodes", perNode), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = r.findConflicts(current, cm, scoped)
			}
		})
	}
}

func TestReconcile_DuplicateOfStaticListenerChainRejected(t *testing.T) {
	l := testListener("tcp", "apps", "01")
	l.Spec.FilterChains = []*lds.FilterChain{{Name: "static"}}
	h := newHarness(t, l, testRoute("catch-all", t0, onListeners("tcp")))
	h.reconcile("catch-all")

	h.requireRejected("catch-all", "Listener 'tcp' spec.filter_chains")
}

func TestReconcile_UnservedRouteReportsWhy(t *testing.T) {
	h := newHarness(t, testRoute("orphan", t0))
	h.reconcile("orphan")
	h.requireRejected("orphan", "no listener in listener_refs exists")

	require.NoError(t, h.client.Create(context.Background(), testListener("https", "apps", "01")))
	h.reconcile("orphan")
	h.requireActive("orphan")

	h.update("orphan", func(r *envoyxdsv1alpha1.Route) { r.Spec.ListenerRefs = nil })
	h.reconcile("orphan")
	h.requireRejected("orphan", "listener_refs not set")
}

func TestReconcile_InvalidEditKeepsEveryNode(t *testing.T) {
	h := newHarness(t,
		testListener("https", "apps", "01,02,03"),
		testRoute("r", t0, inCluster("apps", "01,02"), withMatch(&lds.FilterChainMatch{SourcePrefixRanges: lbRanges})),
	)
	h.reconcile("r")
	require.Equal(t, []string{"apps/01", "apps/02"}, h.placement("r"))

	h.update("r", func(r *envoyxdsv1alpha1.Route) {
		r.Annotations["nodes"] = "02,03"
		r.Spec.FilterChainMatch = &lds.FilterChainMatch{ServerNames: []string{"*"}}
	})
	h.reconcile("r")

	assert.Equal(t, []string{"apps/01", "apps/02"}, h.placement("r"), "an invalid edit must not move the route")
	assert.Contains(t, h.get("r").Status.Message, "partial wildcards")
}

func TestReconcile_NotifiesListenersOnlyOnChange(t *testing.T) {
	h := newHarness(t,
		testListener("https", "apps", "01"),
		testRoute("r", t0, withMatch(sni("shop.example.com"))),
	)
	h.reconcile("r")
	assert.Equal(t, []string{"https"}, drainNames(h.listenerEvents))
	assert.True(t, h.r.Config.ReconciliationStatus.HasPendingListeners())

	h.reconcile("r")
	assert.Empty(t, drainNames(h.listenerEvents), "an unchanged route must not rebuild its listener")

	h.update("r", func(r *envoyxdsv1alpha1.Route) {
		r.Generation++
		r.Spec.StatPrefix = "changed"
	})
	h.reconcile("r")
	assert.Equal(t, []string{"https"}, drainNames(h.listenerEvents))
}

func TestReconcile_DeletedListenerRequeuesItsRoutes(t *testing.T) {
	h := newHarness(t, testRoute("r", t0, onListeners("https", "quic")), testRoute("other", t0, onListeners("http")))

	gone := testListener("https", "apps", "01")
	requests := h.r.routesForListener(context.Background(), gone)
	reqs := make([]string, 0, len(requests))
	for _, req := range requests {
		reqs = append(reqs, req.Name)
	}
	assert.Equal(t, []string{"r"}, reqs, "a deleted listener must still map to the routes that reference it")
}
