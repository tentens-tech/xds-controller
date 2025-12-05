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

package eds

import (
	"testing"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	edstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/eds"
)

func TestEndpointRecast(t *testing.T) {
	tests := []struct {
		name    string
		eds     edstypes.EDS
		wantErr bool
	}{
		{
			name: "basic endpoint config",
			eds: edstypes.EDS{
				ClusterName: "test-cluster",
			},
			wantErr: false,
		},
		{
			name:    "empty endpoint config",
			eds:     edstypes.EDS{},
			wantErr: false,
		},
		{
			name: "endpoint with locality endpoints",
			eds: edstypes.EDS{
				ClusterName: "test-cluster",
				Endpoints: []*edstypes.LocalityLbEndpoints{
					{
						LbEndpoints: []*edstypes.LbEndpoint{
							{
								Endpoint: &edstypes.Endpoint{
									Address: &edstypes.Address{
										SocketAddress: &edstypes.SocketAddress{
											Address:   "127.0.0.1",
											PortValue: 8080,
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EndpointRecast(tt.eds)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

func TestCountEndpoints(t *testing.T) {
	tests := []struct {
		name string
		cla  *endpoint.ClusterLoadAssignment
		want int
	}{
		{
			name: "nil cluster load assignment",
			cla:  nil,
			want: 0,
		},
		{
			name: "empty endpoints",
			cla: &endpoint.ClusterLoadAssignment{
				ClusterName: "test",
				Endpoints:   nil,
			},
			want: 0,
		},
		{
			name: "single locality with one endpoint",
			cla: &endpoint.ClusterLoadAssignment{
				ClusterName: "test",
				Endpoints: []*endpoint.LocalityLbEndpoints{
					{
						LbEndpoints: []*endpoint.LbEndpoint{
							{},
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "single locality with multiple endpoints",
			cla: &endpoint.ClusterLoadAssignment{
				ClusterName: "test",
				Endpoints: []*endpoint.LocalityLbEndpoints{
					{
						LbEndpoints: []*endpoint.LbEndpoint{
							{},
							{},
							{},
						},
					},
				},
			},
			want: 3,
		},
		{
			name: "multiple localities with multiple endpoints",
			cla: &endpoint.ClusterLoadAssignment{
				ClusterName: "test",
				Endpoints: []*endpoint.LocalityLbEndpoints{
					{
						LbEndpoints: []*endpoint.LbEndpoint{
							{},
							{},
						},
					},
					{
						LbEndpoints: []*endpoint.LbEndpoint{
							{},
						},
					},
					{
						LbEndpoints: []*endpoint.LbEndpoint{
							{},
							{},
							{},
						},
					},
				},
			},
			want: 6,
		},
		{
			name: "locality with nil lb endpoints",
			cla: &endpoint.ClusterLoadAssignment{
				ClusterName: "test",
				Endpoints: []*endpoint.LocalityLbEndpoints{
					{
						LbEndpoints: nil,
					},
				},
			},
			want: 0,
		},
		{
			name: "nil locality in slice",
			cla: &endpoint.ClusterLoadAssignment{
				ClusterName: "test",
				Endpoints: []*endpoint.LocalityLbEndpoints{
					nil,
					{
						LbEndpoints: []*endpoint.LbEndpoint{
							{},
						},
					},
				},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countEndpoints(tt.cla)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestEndpointStatusEqual(t *testing.T) {
	tests := []struct {
		name   string
		a      envoyxdsv1alpha1.EndpointStatus
		b      envoyxdsv1alpha1.EndpointStatus
		wantEq bool
	}{
		{
			name:   "empty statuses are equal",
			a:      envoyxdsv1alpha1.EndpointStatus{},
			b:      envoyxdsv1alpha1.EndpointStatus{},
			wantEq: true,
		},
		{
			name: "identical statuses are equal",
			a: envoyxdsv1alpha1.EndpointStatus{
				Active:             true,
				EndpointCount:      5,
				Nodes:              "node1",
				Clusters:           "cluster1",
				ObservedGeneration: 1,
				Snapshots: []envoyxdsv1alpha1.SnapshotInfo{
					{NodeID: "node1", Cluster: "cluster1", Active: true},
				},
			},
			b: envoyxdsv1alpha1.EndpointStatus{
				Active:             true,
				EndpointCount:      5,
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
			a: envoyxdsv1alpha1.EndpointStatus{
				Active: true,
			},
			b: envoyxdsv1alpha1.EndpointStatus{
				Active: false,
			},
			wantEq: false,
		},
		{
			name: "different endpoint count",
			a: envoyxdsv1alpha1.EndpointStatus{
				EndpointCount: 5,
			},
			b: envoyxdsv1alpha1.EndpointStatus{
				EndpointCount: 10,
			},
			wantEq: false,
		},
		{
			name: "different nodes",
			a: envoyxdsv1alpha1.EndpointStatus{
				Nodes: "node1",
			},
			b: envoyxdsv1alpha1.EndpointStatus{
				Nodes: "node1,node2",
			},
			wantEq: false,
		},
		{
			name: "different clusters",
			a: envoyxdsv1alpha1.EndpointStatus{
				Clusters: "cluster1",
			},
			b: envoyxdsv1alpha1.EndpointStatus{
				Clusters: "cluster1,cluster2",
			},
			wantEq: false,
		},
		{
			name: "different observed generation",
			a: envoyxdsv1alpha1.EndpointStatus{
				ObservedGeneration: 1,
			},
			b: envoyxdsv1alpha1.EndpointStatus{
				ObservedGeneration: 2,
			},
			wantEq: false,
		},
		{
			name: "different snapshot count",
			a: envoyxdsv1alpha1.EndpointStatus{
				Snapshots: []envoyxdsv1alpha1.SnapshotInfo{
					{NodeID: "node1"},
				},
			},
			b: envoyxdsv1alpha1.EndpointStatus{
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
			result := endpointStatusEqual(tt.a, tt.b)
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
				Message:            "Endpoint is active",
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
					Message:            "Endpoint is inactive",
				},
			},
			newCondition: metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "Active",
				Message:            "Endpoint is active",
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
					Message:            "Endpoint is active",
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

func TestEndpointReconciler_Integration(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))

	// Create a test endpoint CR
	testEndpoint := &envoyxdsv1alpha1.Endpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-endpoint",
			Namespace: "default",
		},
		Spec: envoyxdsv1alpha1.EndpointSpec{
			EDS: edstypes.EDS{
				ClusterName: "test-cluster",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(testEndpoint).
		WithStatusSubresource(testEndpoint).
		Build()

	reconciler := &EndpointReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Config: &xds.Config{
			NodeID:               "test-node",
			Cluster:              "test-cluster",
			EndpointConfigs:      make(map[string][]*endpoint.ClusterLoadAssignment),
			ReconciliationStatus: status.NewReconciliationStatus(),
		},
	}

	// Verify reconciler was created correctly
	assert.NotNil(t, reconciler)
	assert.NotNil(t, reconciler.Config)
	assert.Equal(t, "test-node", reconciler.Config.NodeID)
}

func TestEndpointReconciler_EndpointConfigs(t *testing.T) {
	config := &xds.Config{
		EndpointConfigs: make(map[string][]*endpoint.ClusterLoadAssignment),
	}

	// Test adding an endpoint config
	nodeID := "clusters=test-cluster;nodes=test-node"
	config.EndpointConfigs[nodeID] = []*endpoint.ClusterLoadAssignment{
		{ClusterName: "cluster1"},
	}

	assert.Len(t, config.EndpointConfigs[nodeID], 1)
	assert.Equal(t, "cluster1", config.EndpointConfigs[nodeID][0].ClusterName)

	// Test removing an endpoint config
	config.EndpointConfigs[nodeID] = append(config.EndpointConfigs[nodeID][:0], config.EndpointConfigs[nodeID][1:]...)
	assert.Len(t, config.EndpointConfigs[nodeID], 0)
}

func TestReconciliationStatus_Endpoints(t *testing.T) {
	config := &xds.Config{
		ReconciliationStatus: status.NewReconciliationStatus(),
	}

	// NewReconciliationStatus starts with all reconciled = true (no resources to reconcile)
	assert.True(t, config.ReconciliationStatus.IsEndpointsReconciled())

	// Set to false
	config.ReconciliationStatus.SetEndpointsReconciled(false)
	assert.False(t, config.ReconciliationStatus.IsEndpointsReconciled())

	// Set back to true
	config.ReconciliationStatus.SetEndpointsReconciled(true)
	assert.True(t, config.ReconciliationStatus.IsEndpointsReconciled())
}

func TestEndpointStatusEqual_LastReconciledIgnored(t *testing.T) {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Hour))

	a := envoyxdsv1alpha1.EndpointStatus{
		Active:         true,
		LastReconciled: now,
	}
	b := envoyxdsv1alpha1.EndpointStatus{
		Active:         true,
		LastReconciled: later,
	}

	// LastReconciled should be ignored in comparison
	assert.True(t, endpointStatusEqual(a, b))
}

func TestEndpointRecast_WithNamedEndpoints(t *testing.T) {
	eds := edstypes.EDS{
		ClusterName: "test-cluster",
		NamedEndpoints: map[string]*edstypes.Endpoint{
			"endpoint1": {
				Address: &edstypes.Address{
					SocketAddress: &edstypes.SocketAddress{
						Address:   "10.0.0.1",
						PortValue: 8080,
					},
				},
			},
		},
	}

	result, err := EndpointRecast(eds)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "test-cluster", result.ClusterName)
}

func ptrUint32(v uint32) *uint32 {
	return &v
}

func TestEndpointRecast_WithPolicy(t *testing.T) {
	eds := edstypes.EDS{
		ClusterName: "test-cluster",
		Policy: &edstypes.ClusterLoadAssignment_Policy{
			OverprovisioningFactor: ptrUint32(140),
		},
	}

	result, err := EndpointRecast(eds)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, result.Policy)
}

func TestEndpointRecast_WithLocality(t *testing.T) {
	eds := edstypes.EDS{
		ClusterName: "test-cluster",
		Endpoints: []*edstypes.LocalityLbEndpoints{
			{
				Locality: &edstypes.Locality{
					Region: "us-east-1",
					Zone:   "us-east-1a",
				},
				LbEndpoints: []*edstypes.LbEndpoint{
					{
						Endpoint: &edstypes.Endpoint{
							Address: &edstypes.Address{
								SocketAddress: &edstypes.SocketAddress{
									Address:   "10.0.0.1",
									PortValue: 8080,
								},
							},
						},
					},
				},
			},
		},
	}

	result, err := EndpointRecast(eds)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Endpoints, 1)
}

func TestCountEndpoints_MixedLocalities(t *testing.T) {
	cla := &endpoint.ClusterLoadAssignment{
		ClusterName: "test",
		Endpoints: []*endpoint.LocalityLbEndpoints{
			{
				LbEndpoints: []*endpoint.LbEndpoint{
					{}, {},
				},
			},
			nil, // nil locality should be handled
			{
				LbEndpoints: nil, // nil lb endpoints should be handled
			},
			{
				LbEndpoints: []*endpoint.LbEndpoint{
					{},
				},
			},
		},
	}

	result := countEndpoints(cla)
	assert.Equal(t, 3, result)
}
