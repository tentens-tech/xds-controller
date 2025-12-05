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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
	rdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/rds"
	routetypes "github.com/tentens-tech/xds-controller/pkg/xds/types/route"
)

func TestRouteRecast(t *testing.T) {
	tests := []struct {
		name    string
		route   routetypes.Route
		wantErr bool
	}{
		{
			name: "basic route config",
			route: routetypes.Route{
				RouteConfig: &rdstypes.RDS{
					VirtualHosts: []*rdstypes.VirtualHost{
						{
							Name:    "test-vhost",
							Domains: []string{"example.com"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty route config with initialized RDS",
			route: routetypes.Route{
				RouteConfig: &rdstypes.RDS{},
			},
			wantErr: false,
		},
		{
			name:    "nil route config should error",
			route:   routetypes.Route{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RouteRecast(tt.route)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

func TestMatchDomainName(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		domain  string
		want    bool
	}{
		{
			name:    "exact match",
			pattern: "example.com",
			domain:  "example.com",
			want:    true,
		},
		{
			name:    "no match",
			pattern: "example.com",
			domain:  "other.com",
			want:    false,
		},
		{
			name:    "wildcard match subdomain",
			pattern: "*.example.com",
			domain:  "www.example.com",
			want:    true,
		},
		{
			name:    "wildcard match different subdomain",
			pattern: "*.example.com",
			domain:  "api.example.com",
			want:    true,
		},
		{
			name:    "wildcard no match - wrong base domain",
			pattern: "*.example.com",
			domain:  "www.other.com",
			want:    false,
		},
		{
			name:    "wildcard no match - base domain itself",
			pattern: "*.example.com",
			domain:  "example.com",
			want:    false,
		},
		{
			name:    "trailing dot exact match",
			pattern: "example.com.",
			domain:  "example.com",
			want:    true,
		},
		{
			name:    "trailing dot on domain",
			pattern: "example.com",
			domain:  "example.com.",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchDomainName(tt.pattern, tt.domain)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestHasVirtualHostOverlap(t *testing.T) {
	tests := []struct {
		name string
		vh1  *rdstypes.VirtualHost
		vh2  *rdstypes.VirtualHost
		want bool
	}{
		{
			name: "overlapping domains",
			vh1: &rdstypes.VirtualHost{
				Name:    "vh1",
				Domains: []string{"example.com", "test.com"},
			},
			vh2: &rdstypes.VirtualHost{
				Name:    "vh2",
				Domains: []string{"example.com", "other.com"},
			},
			want: true,
		},
		{
			name: "no overlap",
			vh1: &rdstypes.VirtualHost{
				Name:    "vh1",
				Domains: []string{"example.com"},
			},
			vh2: &rdstypes.VirtualHost{
				Name:    "vh2",
				Domains: []string{"other.com"},
			},
			want: false,
		},
		{
			name: "nil vh1",
			vh1:  nil,
			vh2: &rdstypes.VirtualHost{
				Name:    "vh2",
				Domains: []string{"example.com"},
			},
			want: false,
		},
		{
			name: "nil vh2",
			vh1: &rdstypes.VirtualHost{
				Name:    "vh1",
				Domains: []string{"example.com"},
			},
			vh2:  nil,
			want: false,
		},
		{
			name: "both nil",
			vh1:  nil,
			vh2:  nil,
			want: false,
		},
		{
			name: "empty domains",
			vh1: &rdstypes.VirtualHost{
				Name:    "vh1",
				Domains: []string{},
			},
			vh2: &rdstypes.VirtualHost{
				Name:    "vh2",
				Domains: []string{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasVirtualHostOverlap(tt.vh1, tt.vh2)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestHasFilterChainOverlap(t *testing.T) {
	port80 := uint32(80)
	port443 := uint32(443)

	tests := []struct {
		name   string
		match1 *lds.FilterChainMatch
		match2 *lds.FilterChainMatch
		want   bool
	}{
		{
			name:   "both nil",
			match1: nil,
			match2: nil,
			want:   false,
		},
		{
			name:   "one nil",
			match1: nil,
			match2: &lds.FilterChainMatch{},
			want:   false,
		},
		{
			name: "same server names",
			match1: &lds.FilterChainMatch{
				ServerNames: []string{"example.com"},
			},
			match2: &lds.FilterChainMatch{
				ServerNames: []string{"example.com"},
			},
			want: true,
		},
		{
			name: "different server names",
			match1: &lds.FilterChainMatch{
				ServerNames: []string{"example.com"},
			},
			match2: &lds.FilterChainMatch{
				ServerNames: []string{"other.com"},
			},
			want: false,
		},
		{
			name: "different ports",
			match1: &lds.FilterChainMatch{
				DestinationPort: &port80,
			},
			match2: &lds.FilterChainMatch{
				DestinationPort: &port443,
			},
			want: false,
		},
		{
			name: "wildcard domain overlap",
			match1: &lds.FilterChainMatch{
				ServerNames: []string{"*.example.com"},
			},
			match2: &lds.FilterChainMatch{
				ServerNames: []string{"api.example.com"},
			},
			want: true,
		},
		{
			name: "empty server names - different types",
			match1: &lds.FilterChainMatch{
				ServerNames: []string{"example.com"},
			},
			match2: &lds.FilterChainMatch{
				ServerNames: []string{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasFilterChainOverlap(tt.match1, tt.match2)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestRouteStatusEqual(t *testing.T) {
	tests := []struct {
		name   string
		a      envoyxdsv1alpha1.RouteStatus
		b      envoyxdsv1alpha1.RouteStatus
		wantEq bool
	}{
		{
			name:   "empty statuses are equal",
			a:      envoyxdsv1alpha1.RouteStatus{},
			b:      envoyxdsv1alpha1.RouteStatus{},
			wantEq: true,
		},
		{
			name: "identical statuses are equal",
			a: envoyxdsv1alpha1.RouteStatus{
				Active:             true,
				VirtualHostCount:   3,
				Listeners:          "listener1",
				Nodes:              "node1",
				Clusters:           "cluster1",
				ObservedGeneration: 1,
			},
			b: envoyxdsv1alpha1.RouteStatus{
				Active:             true,
				VirtualHostCount:   3,
				Listeners:          "listener1",
				Nodes:              "node1",
				Clusters:           "cluster1",
				ObservedGeneration: 1,
			},
			wantEq: true,
		},
		{
			name: "different active status",
			a: envoyxdsv1alpha1.RouteStatus{
				Active: true,
			},
			b: envoyxdsv1alpha1.RouteStatus{
				Active: false,
			},
			wantEq: false,
		},
		{
			name: "different virtual host count",
			a: envoyxdsv1alpha1.RouteStatus{
				VirtualHostCount: 3,
			},
			b: envoyxdsv1alpha1.RouteStatus{
				VirtualHostCount: 5,
			},
			wantEq: false,
		},
		{
			name: "different listeners",
			a: envoyxdsv1alpha1.RouteStatus{
				Listeners: "listener1",
			},
			b: envoyxdsv1alpha1.RouteStatus{
				Listeners: "listener1,listener2",
			},
			wantEq: false,
		},
		{
			name: "different nodes",
			a: envoyxdsv1alpha1.RouteStatus{
				Nodes: "node1",
			},
			b: envoyxdsv1alpha1.RouteStatus{
				Nodes: "node1,node2",
			},
			wantEq: false,
		},
		{
			name: "different clusters",
			a: envoyxdsv1alpha1.RouteStatus{
				Clusters: "cluster1",
			},
			b: envoyxdsv1alpha1.RouteStatus{
				Clusters: "cluster1,cluster2",
			},
			wantEq: false,
		},
		{
			name: "different observed generation",
			a: envoyxdsv1alpha1.RouteStatus{
				ObservedGeneration: 1,
			},
			b: envoyxdsv1alpha1.RouteStatus{
				ObservedGeneration: 2,
			},
			wantEq: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := routeStatusEqual(tt.a, tt.b)
			assert.Equal(t, tt.wantEq, result)
		})
	}
}

func TestUpdateRouteCondition(t *testing.T) {
	now := metav1.Now()
	tests := []struct {
		name         string
		conditions   []metav1.Condition
		newCondition metav1.Condition
		wantLen      int
	}{
		{
			name:       "add new condition to empty slice",
			conditions: []metav1.Condition{},
			newCondition: metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "Active",
				Message:            "Route is active",
			},
			wantLen: 1,
		},
		{
			name: "update existing condition",
			conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					LastTransitionTime: now,
					Reason:             "Inactive",
					Message:            "Route is inactive",
				},
			},
			newCondition: metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "Active",
				Message:            "Route is active",
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := updateRouteCondition(tt.conditions, tt.newCondition)
			assert.Len(t, result, tt.wantLen)

			found := false
			for _, c := range result {
				if c.Type == tt.newCondition.Type {
					found = true
					assert.Equal(t, tt.newCondition.Status, c.Status)
					assert.Equal(t, tt.newCondition.Reason, c.Reason)
					break
				}
			}
			assert.True(t, found, "condition should be found in result")
		})
	}
}

func TestRouteReconciler_Integration(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))

	testRoute := &envoyxdsv1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: envoyxdsv1alpha1.RouteSpec{
			Route: routetypes.Route{
				ListenerRefs: []string{"test-listener"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(testRoute).
		WithStatusSubresource(testRoute).
		Build()

	config := &xds.Config{
		NodeID:               "test-node",
		Cluster:              "test-cluster",
		RouteConfigs:         make(map[string][]*xds.RouteConfig),
		ReconciliationStatus: status.NewReconciliationStatus(),
	}

	reconciler := &RouteReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Config: config,
	}

	assert.NotNil(t, reconciler)
	assert.NotNil(t, reconciler.Config)
	assert.Equal(t, "test-node", reconciler.Config.NodeID)
}

func TestIsContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		element  string
		expected bool
	}{
		{
			name:     "element exists",
			slice:    []string{"a", "b", "c"},
			element:  "b",
			expected: true,
		},
		{
			name:     "element does not exist",
			slice:    []string{"a", "b", "c"},
			element:  "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			element:  "a",
			expected: false,
		},
		{
			name:     "empty element",
			slice:    []string{"a", "b", ""},
			element:  "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isContains(tt.slice, tt.element)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReconciliationStatus_Routes(t *testing.T) {
	config := &xds.Config{
		ReconciliationStatus: status.NewReconciliationStatus(),
	}

	// NewReconciliationStatus starts with all reconciled = true (no resources to reconcile)
	assert.True(t, config.ReconciliationStatus.IsRoutesReconciled())

	// Set to false
	config.ReconciliationStatus.SetRoutesReconciled(false)
	assert.False(t, config.ReconciliationStatus.IsRoutesReconciled())

	// Set back to true
	config.ReconciliationStatus.SetRoutesReconciled(true)
	assert.True(t, config.ReconciliationStatus.IsRoutesReconciled())
}

func TestRouteStatusEqual_LastReconciledIgnored(t *testing.T) {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Hour))

	a := envoyxdsv1alpha1.RouteStatus{
		Active:         true,
		LastReconciled: now,
	}
	b := envoyxdsv1alpha1.RouteStatus{
		Active:         true,
		LastReconciled: later,
	}

	assert.True(t, routeStatusEqual(a, b))
}

func TestHasRouteConfigOverlap(t *testing.T) {
	tests := []struct {
		name   string
		route1 *routetypes.Route
		route2 *routetypes.Route
		match1 *lds.FilterChainMatch
		match2 *lds.FilterChainMatch
		want   bool
	}{
		{
			name:   "both routes nil",
			route1: nil,
			route2: nil,
			match1: nil,
			match2: nil,
			want:   false,
		},
		{
			name:   "one route nil",
			route1: nil,
			route2: &routetypes.Route{},
			match1: nil,
			match2: nil,
			want:   false,
		},
		{
			name: "routes with nil route configs",
			route1: &routetypes.Route{
				RouteConfig: nil,
			},
			route2: &routetypes.Route{
				RouteConfig: nil,
			},
			match1: nil,
			match2: nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasRouteConfigOverlap(tt.route1, tt.route2, tt.match1, tt.match2)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestRouteRecast_WithVirtualHosts(t *testing.T) {
	route := routetypes.Route{
		RouteConfig: &rdstypes.RDS{
			VirtualHosts: []*rdstypes.VirtualHost{
				{
					Name:    "vhost1",
					Domains: []string{"example.com"},
					Routes: []*rdstypes.Route{
						{
							Name: "route1",
						},
					},
				},
			},
		},
	}

	result, err := RouteRecast(route)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.VirtualHosts, 1)
	assert.Equal(t, "vhost1", result.VirtualHosts[0].Name)
}
