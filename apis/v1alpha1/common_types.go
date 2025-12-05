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
)

// SnapshotInfo contains information about a snapshot where the resource exists
type SnapshotInfo struct {
	// NodeID is the Envoy node identifier
	NodeID string `json:"nodeId"`
	// Cluster is the Envoy cluster name
	Cluster string `json:"cluster,omitempty"`
	// Version is the snapshot version hash
	Version string `json:"version,omitempty"`
	// Active indicates if the resource is active in this snapshot
	Active bool `json:"active"`
	// LastUpdated is when this snapshot was last updated
	LastUpdated metav1.Time `json:"lastUpdated,omitempty"`
}
