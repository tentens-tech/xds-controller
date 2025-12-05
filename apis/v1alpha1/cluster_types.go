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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tentens-tech/xds-controller/pkg/xds/types/cds"
)

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// EndpointRefs specifies the Endpoint resource(s) this cluster uses for load balancing.
	// Each entry is an Endpoint CR name. The endpoints from all referenced resources
	// will be merged into the cluster's load_assignment.
	// This is optional - you can still use inline load_assignment in the CDS spec.
	// +optional
	EndpointRefs []string `json:"endpoint_refs,omitempty"`

	// CDS contains the native Envoy cluster configuration
	cds.CDS `json:""`
}

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	// Active indicates if the cluster is currently active in any snapshot
	// +optional
	Active bool `json:"active,omitempty"`

	// EndpointCount is the total number of endpoints from referenced Endpoint resources
	// +optional
	EndpointCount int `json:"endpointCount,omitempty"`

	// ReferencedEndpoints is a comma-separated list of Endpoint resources referenced by this cluster
	// +optional
	ReferencedEndpoints string `json:"referencedEndpoints,omitempty"`

	// Snapshots contains information about snapshots where this cluster exists
	// +optional
	Snapshots []SnapshotInfo `json:"snapshots,omitempty"`

	// Nodes is a comma-separated list of node IDs where this cluster is deployed
	// +optional
	Nodes string `json:"nodes,omitempty"`

	// Clusters is a comma-separated list of cluster names where this cluster is deployed
	// +optional
	Clusters string `json:"clusters,omitempty"`

	// LastReconciled is the last time the cluster was successfully reconciled
	// +optional
	LastReconciled metav1.Time `json:"lastReconciled,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the cluster's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Message provides additional information about the current state
	// +optional
	Message string `json:"message,omitempty"`
}

// Condition types for Cluster
const (
	// ClusterConditionReady indicates the cluster is ready and active
	ClusterConditionReady = "Ready"
	// ClusterConditionReconciled indicates the cluster has been successfully reconciled
	ClusterConditionReconciled = "Reconciled"
	// ClusterConditionError indicates there was an error during reconciliation
	ClusterConditionError = "Error"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Active",type="boolean",JSONPath=".status.active",description="Whether the cluster is active"
//+kubebuilder:printcolumn:name="Endpoints",type="integer",JSONPath=".status.endpointCount",description="Number of endpoints"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.message",priority=0,description="Status message"
//+kubebuilder:printcolumn:name="Nodes",type="string",JSONPath=".status.nodes",description="Nodes where cluster is deployed"
//+kubebuilder:printcolumn:name="Clusters",type="string",JSONPath=".status.clusters",description="Clusters where cluster is deployed"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Cluster is the Schema for the clusters API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
