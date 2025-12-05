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
	"testing"
	"time"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
)

func TestHasFilterChainDuplicates(t *testing.T) {
	tests := []struct {
		name          string
		domains       []string
		searchDomains []string
		want          bool
	}{
		{
			name:          "no overlap",
			domains:       []string{"example.com", "test.com"},
			searchDomains: []string{"other.com", "another.com"},
			want:          false,
		},
		{
			name:          "exact match",
			domains:       []string{"example.com", "test.com"},
			searchDomains: []string{"example.com"},
			want:          true,
		},
		{
			name:          "multiple matches",
			domains:       []string{"example.com", "test.com"},
			searchDomains: []string{"example.com", "test.com"},
			want:          true,
		},
		{
			name:          "empty domains",
			domains:       []string{},
			searchDomains: []string{"example.com"},
			want:          false,
		},
		{
			name:          "empty search domains",
			domains:       []string{"example.com"},
			searchDomains: []string{},
			want:          false,
		},
		{
			name:          "both empty",
			domains:       []string{},
			searchDomains: []string{},
			want:          false,
		},
		{
			name:          "partial match in longer list",
			domains:       []string{"a.com", "b.com", "c.com"},
			searchDomains: []string{"d.com", "b.com", "e.com"},
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasFilterChainDuplicates(tt.domains, tt.searchDomains)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestConfigSource(t *testing.T) {
	result := configSource()
	assert.NotNil(t, result)
	assert.NotNil(t, result.ConfigSourceSpecifier)
	assert.NotNil(t, result.GetAds())
}

func TestListenerStatusEqual(t *testing.T) {
	tests := []struct {
		name   string
		a      envoyxdsv1alpha1.ListenerStatus
		b      envoyxdsv1alpha1.ListenerStatus
		wantEq bool
	}{
		{
			name:   "empty statuses are equal",
			a:      envoyxdsv1alpha1.ListenerStatus{},
			b:      envoyxdsv1alpha1.ListenerStatus{},
			wantEq: true,
		},
		{
			name: "identical statuses are equal",
			a: envoyxdsv1alpha1.ListenerStatus{
				Active:             true,
				FilterChainCount:   3,
				Nodes:              "node1",
				Clusters:           "cluster1",
				ObservedGeneration: 1,
				Snapshots: []envoyxdsv1alpha1.SnapshotInfo{
					{NodeID: "node1", Cluster: "cluster1", Active: true},
				},
			},
			b: envoyxdsv1alpha1.ListenerStatus{
				Active:             true,
				FilterChainCount:   3,
				Nodes:              "node1",
				Clusters:           "cluster1",
				ObservedGeneration: 1,
				Snapshots: []envoyxdsv1alpha1.SnapshotInfo{
					{NodeID: "node1", Cluster: "cluster1", Active: true},
				},
			},
			wantEq: true,
		},
		{
			name: "different active status",
			a: envoyxdsv1alpha1.ListenerStatus{
				Active: true,
			},
			b: envoyxdsv1alpha1.ListenerStatus{
				Active: false,
			},
			wantEq: false,
		},
		{
			name: "different filter chain count",
			a: envoyxdsv1alpha1.ListenerStatus{
				FilterChainCount: 3,
			},
			b: envoyxdsv1alpha1.ListenerStatus{
				FilterChainCount: 5,
			},
			wantEq: false,
		},
		{
			name: "different nodes",
			a: envoyxdsv1alpha1.ListenerStatus{
				Nodes: "node1",
			},
			b: envoyxdsv1alpha1.ListenerStatus{
				Nodes: "node1,node2",
			},
			wantEq: false,
		},
		{
			name: "different clusters",
			a: envoyxdsv1alpha1.ListenerStatus{
				Clusters: "cluster1",
			},
			b: envoyxdsv1alpha1.ListenerStatus{
				Clusters: "cluster1,cluster2",
			},
			wantEq: false,
		},
		{
			name: "different observed generation",
			a: envoyxdsv1alpha1.ListenerStatus{
				ObservedGeneration: 1,
			},
			b: envoyxdsv1alpha1.ListenerStatus{
				ObservedGeneration: 2,
			},
			wantEq: false,
		},
		{
			name: "different snapshot count",
			a: envoyxdsv1alpha1.ListenerStatus{
				Snapshots: []envoyxdsv1alpha1.SnapshotInfo{
					{NodeID: "node1"},
				},
			},
			b: envoyxdsv1alpha1.ListenerStatus{
				Snapshots: []envoyxdsv1alpha1.SnapshotInfo{
					{NodeID: "node1"},
					{NodeID: "node2"},
				},
			},
			wantEq: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := listenerStatusEqual(tt.a, tt.b)
			assert.Equal(t, tt.wantEq, result)
		})
	}
}

func TestUpdateCondition(t *testing.T) {
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
				Message:            "Listener is active",
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
					Message:            "Listener is inactive",
				},
			},
			newCondition: metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "Active",
				Message:            "Listener is active",
			},
			wantLen: 1,
		},
		{
			name: "add new condition to existing slice",
			conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             "Active",
					Message:            "Listener is active",
				},
			},
			newCondition: metav1.Condition{
				Type:               "Reconciled",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "Reconciled",
				Message:            "Successfully reconciled",
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := updateCondition(tt.conditions, tt.newCondition)
			assert.Len(t, result, tt.wantLen)

			// Verify the condition was added/updated
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

func TestListenerReconciler_Integration(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))

	// Create a test listener CR
	testListener := &envoyxdsv1alpha1.Listener{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-listener",
			Namespace: "default",
		},
		Spec: envoyxdsv1alpha1.ListenerSpec{
			LDS: lds.LDS{
				Address: &lds.Address{
					SocketAddress: &lds.SocketAddress{
						Address:   "0.0.0.0",
						PortValue: 8080,
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(testListener).
		WithStatusSubresource(testListener).
		Build()

	reconciler := &ListenerReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Config: &xds.Config{
			NodeID:               "test-node",
			Cluster:              "test-cluster",
			ListenerConfigs:      make(map[string][]*listener.Listener),
			RouteConfigs:         make(map[string][]*xds.RouteConfig),
			ReconciliationStatus: status.NewReconciliationStatus(),
		},
	}

	// Verify reconciler was created correctly
	assert.NotNil(t, reconciler)
	assert.NotNil(t, reconciler.Config)
	assert.Equal(t, "test-node", reconciler.Config.NodeID)
}

func TestListenerReconciler_ListenerConfigs(t *testing.T) {
	config := &xds.Config{
		ListenerConfigs: make(map[string][]*listener.Listener),
	}

	// Test adding a listener config
	nodeID := "clusters=test-cluster;nodes=test-node"
	config.ListenerConfigs[nodeID] = []*listener.Listener{
		{Name: "listener1"},
	}

	assert.Len(t, config.ListenerConfigs[nodeID], 1)
	assert.Equal(t, "listener1", config.ListenerConfigs[nodeID][0].Name)

	// Test removing a listener config
	config.ListenerConfigs[nodeID] = append(config.ListenerConfigs[nodeID][:0], config.ListenerConfigs[nodeID][1:]...)
	assert.Len(t, config.ListenerConfigs[nodeID], 0)
}

func TestReconciliationStatus_Listeners(t *testing.T) {
	config := &xds.Config{
		ReconciliationStatus: status.NewReconciliationStatus(),
	}

	// NewReconciliationStatus starts with all reconciled = true (no resources to reconcile)
	assert.True(t, config.ReconciliationStatus.IsListenersReconciled())

	// Set to false
	config.ReconciliationStatus.SetListenersReconciled(false)
	assert.False(t, config.ReconciliationStatus.IsListenersReconciled())

	// Set back to true
	config.ReconciliationStatus.SetListenersReconciled(true)
	assert.True(t, config.ReconciliationStatus.IsListenersReconciled())
}

func TestListenerStatusEqual_LastReconciledIgnored(t *testing.T) {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Hour))

	a := envoyxdsv1alpha1.ListenerStatus{
		Active:         true,
		LastReconciled: now,
	}
	b := envoyxdsv1alpha1.ListenerStatus{
		Active:         true,
		LastReconciled: later,
	}

	// LastReconciled should be ignored in comparison
	assert.True(t, listenerStatusEqual(a, b))
}

func TestListenerRecast_Basic(t *testing.T) {
	listenerCR := envoyxdsv1alpha1.Listener{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-listener",
			Namespace: "default",
		},
		Spec: envoyxdsv1alpha1.ListenerSpec{
			LDS: lds.LDS{
				Address: &lds.Address{
					SocketAddress: &lds.SocketAddress{
						Address:   "0.0.0.0",
						PortValue: 8080,
					},
				},
				FilterChains: []*lds.FilterChain{
					{
						Filters: []*lds.Filter{},
					},
				},
			},
		},
	}

	result, err := ListenerRecast(listenerCR, []*envoyxdsv1alpha1.Route{}, "test-node")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "test-listener", result.Name)
}

func TestListenerRecast_NoRoutesNoFilters(t *testing.T) {
	listenerCR := envoyxdsv1alpha1.Listener{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-listener",
			Namespace: "default",
		},
		Spec: envoyxdsv1alpha1.ListenerSpec{
			LDS: lds.LDS{
				Address: &lds.Address{
					SocketAddress: &lds.SocketAddress{
						Address:   "0.0.0.0",
						PortValue: 8080,
					},
				},
			},
		},
	}

	_, err := ListenerRecast(listenerCR, []*envoyxdsv1alpha1.Route{}, "test-node")
	// Should return error because no routes or filters
	assert.Error(t, err)
}

func TestGetNodesForListener(t *testing.T) {
	config := &xds.Config{
		NodeID:               "default-node",
		Cluster:              "default-cluster",
		ReconciliationStatus: status.NewReconciliationStatus(),
	}

	reconciler := &ListenerReconciler{
		Config: config,
	}

	tests := []struct {
		name       string
		listener   *envoyxdsv1alpha1.Listener
		wantMinLen int
	}{
		{
			name: "listener with no annotations",
			listener: &envoyxdsv1alpha1.Listener{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-listener",
					Namespace: "default",
				},
			},
			wantMinLen: 1,
		},
		{
			name: "listener with annotations",
			listener: &envoyxdsv1alpha1.Listener{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-listener",
					Namespace: "default",
					Annotations: map[string]string{
						"nodes":    "node1,node2",
						"clusters": "cluster1",
					},
				},
			},
			wantMinLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := reconciler.getNodesForListener(tt.listener)
			assert.GreaterOrEqual(t, len(nodes), tt.wantMinLen)
		})
	}
}

func TestErrorDuplicateFound(t *testing.T) {
	assert.NotNil(t, ErrorDuplicateFound)
	assert.Error(t, ErrorDuplicateFound)
	assert.Contains(t, ErrorDuplicateFound.Error(), "duplicate found")
}

func TestHasFilterChainDuplicates_WildcardDomains(t *testing.T) {
	tests := []struct {
		name          string
		domains       []string
		searchDomains []string
		want          bool
	}{
		{
			name:          "wildcard match",
			domains:       []string{"*.example.com"},
			searchDomains: []string{"*.example.com"},
			want:          true,
		},
		{
			name:          "wildcard no match",
			domains:       []string{"*.example.com"},
			searchDomains: []string{"*.other.com"},
			want:          false,
		},
		{
			name:          "mixed with wildcards",
			domains:       []string{"example.com", "*.test.com"},
			searchDomains: []string{"*.test.com"},
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasFilterChainDuplicates(tt.domains, tt.searchDomains)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestUpdateCondition_StatusTransition(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-time.Hour))
	newTime := metav1.Now()

	// Test that LastTransitionTime is updated when status changes
	conditions := []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			LastTransitionTime: oldTime,
			Reason:             "OldReason",
		},
	}

	newCondition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: newTime,
		Reason:             "NewReason",
	}

	result := updateCondition(conditions, newCondition)
	assert.Len(t, result, 1)
	assert.Equal(t, metav1.ConditionTrue, result[0].Status)
	assert.Equal(t, newTime, result[0].LastTransitionTime)
}

func TestUpdateCondition_SameStatus(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-time.Hour))
	newTime := metav1.Now()

	// Test that LastTransitionTime is preserved when status doesn't change
	conditions := []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: oldTime,
			Reason:             "OldReason",
			Message:            "Old message",
		},
	}

	newCondition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: newTime,
		Reason:             "NewReason",
		Message:            "New message",
	}

	result := updateCondition(conditions, newCondition)
	assert.Len(t, result, 1)
	assert.Equal(t, metav1.ConditionTrue, result[0].Status)
	assert.Equal(t, oldTime, result[0].LastTransitionTime) // Preserved
	assert.Equal(t, "NewReason", result[0].Reason)         // Updated
	assert.Equal(t, "New message", result[0].Message)      // Updated
}
