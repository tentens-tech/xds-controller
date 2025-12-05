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

	"github.com/tentens-tech/xds-controller/pkg/xds/types/route"
)

// RouteSpec defines the desired state of Route
type RouteSpec struct {
	route.Route `json:""`
}

// RouteStatus defines the observed state of Route
type RouteStatus struct {
	// Active indicates if the route is currently active in any snapshot
	// +optional
	Active bool `json:"active,omitempty"`

	// VirtualHostCount is the number of virtual hosts in this route
	// +optional
	VirtualHostCount int `json:"virtualHostCount,omitempty"`

	// Listeners is a comma-separated list of listeners this route is attached to
	// +optional
	Listeners string `json:"listeners,omitempty"`

	// Snapshots contains information about snapshots where this route exists
	// +optional
	Snapshots []SnapshotInfo `json:"snapshots,omitempty"`

	// Nodes is a comma-separated list of node IDs where this route is deployed
	// +optional
	Nodes string `json:"nodes,omitempty"`

	// Clusters is a comma-separated list of cluster names where this route is deployed
	// +optional
	Clusters string `json:"clusters,omitempty"`

	// LastReconciled is the last time the route was successfully reconciled
	// +optional
	LastReconciled metav1.Time `json:"lastReconciled,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the route's state
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

// Condition types for Route
const (
	// RouteConditionReady indicates the route is ready and active
	RouteConditionReady = "Ready"
	// RouteConditionReconciled indicates the route has been successfully reconciled
	RouteConditionReconciled = "Reconciled"
	// RouteConditionError indicates there was an error during reconciliation
	RouteConditionError = "Error"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Active",type="boolean",JSONPath=".status.active",description="Whether the route is active"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.message",priority=0,description="Status message"
//+kubebuilder:printcolumn:name="VirtualHosts",type="integer",JSONPath=".status.virtualHostCount",description="Number of virtual hosts"
//+kubebuilder:printcolumn:name="Listeners",type="string",JSONPath=".status.listeners",description="Attached listeners"
//+kubebuilder:printcolumn:name="Nodes",type="string",JSONPath=".status.nodes",description="Nodes where route is deployed"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Route is the Schema for the routes API
type Route struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteSpec   `json:"spec,omitempty"`
	Status RouteStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RouteList contains a list of Route
type RouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Route `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Route{}, &RouteList{})
}
