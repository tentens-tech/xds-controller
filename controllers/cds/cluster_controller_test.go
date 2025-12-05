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

package cds

import (
	"context"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	cdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/cds"
	edstypes "github.com/tentens-tech/xds-controller/pkg/xds/types/eds"
)

func ptrString(s string) *string {
	return &s
}

func TestClusterRecast(t *testing.T) {
	tests := []struct {
		name    string
		cds     cdstypes.CDS
		wantErr bool
	}{
		{
			name: "basic cluster config",
			cds: cdstypes.CDS{
				ConnectTimeout: ptrString("5s"),
				Type:           "STRICT_DNS",
			},
			wantErr: false,
		},
		{
			name: "cluster with lb policy",
			cds: cdstypes.CDS{
				ConnectTimeout: ptrString("10s"),
				Type:           "LOGICAL_DNS",
				LbPolicy:       "ROUND_ROBIN",
			},
			wantErr: false,
		},
		{
			name:    "empty cluster config",
			cds:     cdstypes.CDS{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ClusterRecast(tt.cds)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

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
				Message:            "Cluster is active",
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
					Message:            "Cluster is inactive",
				},
			},
			newCondition: metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "Active",
				Message:            "Cluster is active",
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
					Message:            "Cluster is active",
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

func TestClusterStatusEqual(t *testing.T) {
	tests := []struct {
		name   string
		a      envoyxdsv1alpha1.ClusterStatus
		b      envoyxdsv1alpha1.ClusterStatus
		wantEq bool
	}{
		{
			name:   "empty statuses are equal",
			a:      envoyxdsv1alpha1.ClusterStatus{},
			b:      envoyxdsv1alpha1.ClusterStatus{},
			wantEq: true,
		},
		{
			name: "identical statuses are equal",
			a: envoyxdsv1alpha1.ClusterStatus{
				Active:              true,
				EndpointCount:       5,
				ReferencedEndpoints: "ep1,ep2",
				Nodes:               "node1",
				Clusters:            "cluster1",
				ObservedGeneration:  1,
				Snapshots: []envoyxdsv1alpha1.SnapshotInfo{
					{NodeID: "node1", Cluster: "cluster1", Active: true},
				},
			},
			b: envoyxdsv1alpha1.ClusterStatus{
				Active:              true,
				EndpointCount:       5,
				ReferencedEndpoints: "ep1,ep2",
				Nodes:               "node1",
				Clusters:            "cluster1",
				ObservedGeneration:  1,
				Snapshots: []envoyxdsv1alpha1.SnapshotInfo{
					{NodeID: "node1", Cluster: "cluster1", Active: true},
				},
			},
			wantEq: true,
		},
		{
			name: "different active status",
			a: envoyxdsv1alpha1.ClusterStatus{
				Active: true,
			},
			b: envoyxdsv1alpha1.ClusterStatus{
				Active: false,
			},
			wantEq: false,
		},
		{
			name: "different endpoint count",
			a: envoyxdsv1alpha1.ClusterStatus{
				EndpointCount: 5,
			},
			b: envoyxdsv1alpha1.ClusterStatus{
				EndpointCount: 10,
			},
			wantEq: false,
		},
		{
			name: "different referenced endpoints",
			a: envoyxdsv1alpha1.ClusterStatus{
				ReferencedEndpoints: "ep1",
			},
			b: envoyxdsv1alpha1.ClusterStatus{
				ReferencedEndpoints: "ep1,ep2",
			},
			wantEq: false,
		},
		{
			name: "different nodes",
			a: envoyxdsv1alpha1.ClusterStatus{
				Nodes: "node1",
			},
			b: envoyxdsv1alpha1.ClusterStatus{
				Nodes: "node1,node2",
			},
			wantEq: false,
		},
		{
			name: "different clusters",
			a: envoyxdsv1alpha1.ClusterStatus{
				Clusters: "cluster1",
			},
			b: envoyxdsv1alpha1.ClusterStatus{
				Clusters: "cluster1,cluster2",
			},
			wantEq: false,
		},
		{
			name: "different observed generation",
			a: envoyxdsv1alpha1.ClusterStatus{
				ObservedGeneration: 1,
			},
			b: envoyxdsv1alpha1.ClusterStatus{
				ObservedGeneration: 2,
			},
			wantEq: false,
		},
		{
			name: "different snapshot count",
			a: envoyxdsv1alpha1.ClusterStatus{
				Snapshots: []envoyxdsv1alpha1.SnapshotInfo{
					{NodeID: "node1"},
				},
			},
			b: envoyxdsv1alpha1.ClusterStatus{
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
			result := clusterStatusEqual(tt.a, tt.b)
			assert.Equal(t, tt.wantEq, result)
		})
	}
}

func TestMergeEndpointRefs(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))

	tests := []struct {
		name            string
		cluster         *envoyxdsv1alpha1.Cluster
		cds             *cluster.Cluster
		endpoints       []envoyxdsv1alpha1.Endpoint
		wantRefsCount   int
		wantEndpointNum int
		wantErr         bool
	}{
		{
			name: "no endpoint refs",
			cluster: &envoyxdsv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.ClusterSpec{
					EndpointRefs: []string{},
				},
			},
			cds:             &cluster.Cluster{Name: "test-cluster"},
			endpoints:       []envoyxdsv1alpha1.Endpoint{},
			wantRefsCount:   0,
			wantEndpointNum: 0,
			wantErr:         false,
		},
		{
			name: "single endpoint ref",
			cluster: &envoyxdsv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.ClusterSpec{
					EndpointRefs: []string{"ep1"},
				},
			},
			cds: &cluster.Cluster{Name: "test-cluster"},
			endpoints: []envoyxdsv1alpha1.Endpoint{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ep1",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.EndpointSpec{
						EDS: edstypes.EDS{
							ClusterName: "test-cluster",
						},
					},
				},
			},
			wantRefsCount:   1,
			wantEndpointNum: 0,
			wantErr:         false,
		},
		{
			name: "endpoint ref not found - should skip",
			cluster: &envoyxdsv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.ClusterSpec{
					EndpointRefs: []string{"nonexistent"},
				},
			},
			cds:             &cluster.Cluster{Name: "test-cluster"},
			endpoints:       []envoyxdsv1alpha1.Endpoint{},
			wantRefsCount:   0,
			wantEndpointNum: 0,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with endpoints
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			for i := range tt.endpoints {
				clientBuilder = clientBuilder.WithObjects(&tt.endpoints[i])
			}
			fakeClient := clientBuilder.Build()

			reconciler := &ClusterReconciler{
				Client: fakeClient,
				Config: &xds.Config{
					NodeID:               "test-node",
					Cluster:              "test-cluster",
					ReconciliationStatus: status.NewReconciliationStatus(),
				},
			}

			ctx := context.Background()
			loadAssignment, refs, count, err := reconciler.mergeEndpointRefs(ctx, tt.cluster, tt.cds)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, refs, tt.wantRefsCount)
			assert.Equal(t, tt.wantEndpointNum, count)

			if tt.wantRefsCount == 0 && len(tt.cluster.Spec.EndpointRefs) == 0 {
				assert.Nil(t, loadAssignment)
			}
		})
	}
}

func TestClusterReconciler_Integration(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))

	// Create a test cluster CR
	testCluster := &envoyxdsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: envoyxdsv1alpha1.ClusterSpec{
			CDS: cdstypes.CDS{
				ConnectTimeout: ptrString("5s"),
				Type:           "STRICT_DNS",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(testCluster).
		WithStatusSubresource(testCluster).
		Build()

	reconciler := &ClusterReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Config: &xds.Config{
			NodeID:               "test-node",
			Cluster:              "test-cluster",
			ClusterConfigs:       make(map[string][]*cluster.Cluster),
			ReconciliationStatus: status.NewReconciliationStatus(),
		},
	}

	// Verify reconciler was created correctly
	assert.NotNil(t, reconciler)
	assert.NotNil(t, reconciler.Config)
	assert.Equal(t, "test-node", reconciler.Config.NodeID)
}

func TestClusterReconciler_ClusterConfigs(t *testing.T) {
	config := &xds.Config{
		ClusterConfigs: make(map[string][]*cluster.Cluster),
	}

	// Test adding a cluster config
	nodeID := "clusters=test-cluster;nodes=test-node"
	config.ClusterConfigs[nodeID] = []*cluster.Cluster{
		{Name: "cluster1"},
	}

	assert.Len(t, config.ClusterConfigs[nodeID], 1)
	assert.Equal(t, "cluster1", config.ClusterConfigs[nodeID][0].Name)

	// Test removing a cluster config
	config.ClusterConfigs[nodeID] = append(config.ClusterConfigs[nodeID][:0], config.ClusterConfigs[nodeID][1:]...)
	assert.Len(t, config.ClusterConfigs[nodeID], 0)
}

func TestClusterRecast_LoadAssignment(t *testing.T) {
	cds := cdstypes.CDS{
		ConnectTimeout: ptrString("5s"),
		Type:           "STATIC",
		LoadAssignment: &cdstypes.ClusterLoadAssignment{
			ClusterName: "static-cluster",
			Endpoints: []*cdstypes.LocalityLbEndpoints{
				{
					LbEndpoints: []*cdstypes.LbEndpoint{
						{
							Endpoint: &cdstypes.Endpoint{
								Address: &cdstypes.Address{
									SocketAddress: &cdstypes.SocketAddress{
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
	}

	result, err := ClusterRecast(cds)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, result.LoadAssignment)
	assert.Equal(t, "static-cluster", result.LoadAssignment.ClusterName)
}

func TestEndpointRecast_WithLocalityEndpoints(t *testing.T) {
	eds := edstypes.EDS{
		ClusterName: "test-cluster",
		Endpoints: []*edstypes.LocalityLbEndpoints{
			{
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
	assert.Equal(t, "test-cluster", result.ClusterName)
}

func TestReconciliationStatus(t *testing.T) {
	config := &xds.Config{
		ReconciliationStatus: status.NewReconciliationStatus(),
	}

	// NewReconciliationStatus starts with all reconciled = true (no resources to reconcile)
	assert.True(t, config.ReconciliationStatus.IsClustersReconciled())

	// Set to false
	config.ReconciliationStatus.SetClustersReconciled(false)
	assert.False(t, config.ReconciliationStatus.IsClustersReconciled())

	// Set back to true
	config.ReconciliationStatus.SetClustersReconciled(true)
	assert.True(t, config.ReconciliationStatus.IsClustersReconciled())
}

func TestConfigCounter(t *testing.T) {
	config := &xds.Config{
		ReconciliationStatus: status.NewReconciliationStatus(),
	}

	assert.Equal(t, uint64(0), config.GetConfigCounter())

	// Increment
	val := config.IncrementConfigCounter()
	assert.Equal(t, uint64(1), val)
	assert.Equal(t, uint64(1), config.GetConfigCounter())

	// Increment again
	val = config.IncrementConfigCounter()
	assert.Equal(t, uint64(2), val)
}

func TestClusterStatusEqual_LastReconciledIgnored(t *testing.T) {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Hour))

	a := envoyxdsv1alpha1.ClusterStatus{
		Active:         true,
		LastReconciled: now,
	}
	b := envoyxdsv1alpha1.ClusterStatus{
		Active:         true,
		LastReconciled: later,
	}

	// LastReconciled should be ignored in comparison
	assert.True(t, clusterStatusEqual(a, b))
}

func TestMergeEndpointRefs_WithExistingLoadAssignment(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))

	// Create cluster with existing load assignment
	existingLoadAssignment := &endpoint.ClusterLoadAssignment{
		ClusterName: "test-cluster",
		Endpoints: []*endpoint.LocalityLbEndpoints{
			{
				LbEndpoints: []*endpoint.LbEndpoint{},
			},
		},
	}

	clusterCR := &envoyxdsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: envoyxdsv1alpha1.ClusterSpec{
			EndpointRefs: []string{"ep1"},
		},
	}

	ep1 := &envoyxdsv1alpha1.Endpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ep1",
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
		WithObjects(ep1).
		Build()

	reconciler := &ClusterReconciler{
		Client: fakeClient,
		Config: &xds.Config{
			NodeID:               "test-node",
			Cluster:              "test-cluster",
			ReconciliationStatus: status.NewReconciliationStatus(),
		},
	}

	cds := &cluster.Cluster{
		Name:           "test-cluster",
		LoadAssignment: existingLoadAssignment,
	}

	ctx := context.Background()
	loadAssignment, refs, _, err := reconciler.mergeEndpointRefs(ctx, clusterCR, cds)

	require.NoError(t, err)
	assert.Len(t, refs, 1)
	if loadAssignment != nil {
		assert.Equal(t, "test-cluster", loadAssignment.ClusterName)
	}
}
