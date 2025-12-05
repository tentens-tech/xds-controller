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

	"github.com/tentens-tech/xds-controller/pkg/xds/types/eds"
)

// EndpointSpec defines the desired state of Endpoint
type EndpointSpec struct {
	eds.EDS `json:""`
}

// EndpointStatus defines the observed state of Endpoint
type EndpointStatus struct {
	// Active indicates if the endpoint is currently active in any snapshot
	// +optional
	Active bool `json:"active,omitempty"`

	// EndpointCount is the total number of lb_endpoints in this resource
	// +optional
	EndpointCount int `json:"endpointCount,omitempty"`

	// ReferencedBy is a comma-separated list of Cluster resources that reference this endpoint
	// +optional
	ReferencedBy string `json:"referencedBy,omitempty"`

	// Snapshots contains information about snapshots where this endpoint exists
	// +optional
	Snapshots []SnapshotInfo `json:"snapshots,omitempty"`

	// Nodes is a comma-separated list of node IDs where this endpoint is deployed
	// +optional
	Nodes string `json:"nodes,omitempty"`

	// Clusters is a comma-separated list of cluster names where this endpoint is deployed
	// +optional
	Clusters string `json:"clusters,omitempty"`

	// LastReconciled is the last time the endpoint was successfully reconciled
	// +optional
	LastReconciled metav1.Time `json:"lastReconciled,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the endpoint's state
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

// Condition types for Endpoint
const (
	// EndpointConditionReady indicates the endpoint is ready and active
	EndpointConditionReady = "Ready"
	// EndpointConditionReconciled indicates the endpoint has been successfully reconciled
	EndpointConditionReconciled = "Reconciled"
	// EndpointConditionError indicates there was an error during reconciliation
	EndpointConditionError = "Error"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="ClusterName",type="string",JSONPath=".spec.cluster_name",description="Cluster name for the endpoint"
//+kubebuilder:printcolumn:name="Active",type="boolean",JSONPath=".status.active",description="Whether the endpoint is active"
//+kubebuilder:printcolumn:name="Endpoints",type="integer",JSONPath=".status.endpointCount",description="Number of endpoints"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.message",priority=0,description="Status message"
//+kubebuilder:printcolumn:name="Nodes",type="string",JSONPath=".status.nodes",description="Nodes where endpoint is deployed"
//+kubebuilder:printcolumn:name="Clusters",type="string",JSONPath=".status.clusters",description="Clusters where endpoint is deployed"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Endpoint is the Schema for the endpoints API
type Endpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EndpointSpec   `json:"spec,omitempty"`
	Status EndpointStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// EndpointList contains a list of Endpoint
type EndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Endpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Endpoint{}, &EndpointList{})
}
