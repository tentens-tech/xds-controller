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

	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
)

// ListenerSpec defines the desired state of Listener
type ListenerSpec struct {
	lds.LDS `json:""`
}

// ListenerStatus defines the observed state of Listener
type ListenerStatus struct {
	// Active indicates if the listener is currently active in any snapshot
	// +optional
	Active bool `json:"active,omitempty"`

	// FilterChainCount is the number of filter chains in this listener
	// +optional
	FilterChainCount int `json:"filterChainCount,omitempty"`

	// Snapshots contains information about snapshots where this listener exists
	// +optional
	Snapshots []SnapshotInfo `json:"snapshots,omitempty"`

	// Nodes is a comma-separated list of node IDs where this listener is deployed
	// +optional
	Nodes string `json:"nodes,omitempty"`

	// Clusters is a comma-separated list of cluster names where this listener is deployed
	// +optional
	Clusters string `json:"clusters,omitempty"`

	// LastReconciled is the last time the listener was successfully reconciled
	// +optional
	LastReconciled metav1.Time `json:"lastReconciled,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the listener's state
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

// Condition types for Listener
const (
	// ListenerConditionReady indicates the listener is ready and active
	ListenerConditionReady = "Ready"
	// ListenerConditionReconciled indicates the listener has been successfully reconciled
	ListenerConditionReconciled = "Reconciled"
	// ListenerConditionError indicates there was an error during reconciliation
	ListenerConditionError = "Error"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Active",type="boolean",JSONPath=".status.active",description="Whether the listener is active"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.message",priority=0,description="Status message"
//+kubebuilder:printcolumn:name="FilterChains",type="integer",JSONPath=".status.filterChainCount",description="Number of filter chains"
//+kubebuilder:printcolumn:name="Nodes",type="string",JSONPath=".status.nodes",description="Nodes where listener is deployed"
//+kubebuilder:printcolumn:name="Clusters",type="string",JSONPath=".status.clusters",description="Clusters where listener is deployed"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Listener is the Schema for the listeners API
type Listener struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ListenerSpec   `json:"spec,omitempty"`
	Status ListenerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ListenerList contains a list of Listener
type ListenerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Listener `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Listener{}, &ListenerList{})
}
