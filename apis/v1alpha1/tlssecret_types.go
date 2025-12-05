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

	xdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types"
)

// TLSSecretSpec defines the desired state of TLSSecret
type TLSSecretSpec struct {
	xdstypes.DomainConfig `json:""`
}

// CertificateInfo contains information about the TLS certificate
type CertificateInfo struct {
	// Issuer is the certificate issuer (e.g., "Let's Encrypt", "Self-Signed", "Vault")
	// +optional
	Issuer string `json:"issuer,omitempty"`

	// Subject is the certificate subject (CN)
	// +optional
	Subject string `json:"subject,omitempty"`

	// DNSNames are the DNS names in the certificate
	// +optional
	DNSNames []string `json:"dnsNames,omitempty"`

	// NotBefore is when the certificate becomes valid
	// +optional
	NotBefore metav1.Time `json:"notBefore,omitempty"`

	// NotAfter is when the certificate expires
	// +optional
	NotAfter metav1.Time `json:"notAfter,omitempty"`

	// DaysUntilExpiry is the number of days until the certificate expires
	// +optional
	DaysUntilExpiry int `json:"daysUntilExpiry,omitempty"`

	// SerialNumber is the certificate serial number
	// +optional
	SerialNumber string `json:"serialNumber,omitempty"`

	// Fingerprint is the SHA256 fingerprint of the certificate
	// +optional
	Fingerprint string `json:"fingerprint,omitempty"`
}

// TLSSecretStatus defines the observed state of TLSSecret
type TLSSecretStatus struct {
	// Active indicates if the secret is currently active in any snapshot
	// +optional
	Active bool `json:"active,omitempty"`

	// CertificateInfo contains information about the TLS certificate
	// +optional
	CertificateInfo *CertificateInfo `json:"certificateInfo,omitempty"`

	// NextRenewal is when the certificate will be renewed
	// +optional
	NextRenewal string `json:"nextRenewal,omitempty"`

	// Snapshots contains information about snapshots where this secret exists
	// +optional
	Snapshots []SnapshotInfo `json:"snapshots,omitempty"`

	// Nodes is a comma-separated list of node IDs where this secret is deployed
	// +optional
	Nodes string `json:"nodes,omitempty"`

	// Clusters is a comma-separated list of cluster names where this secret is deployed
	// +optional
	Clusters string `json:"clusters,omitempty"`

	// LastReconciled is the last time the secret was successfully reconciled
	// +optional
	LastReconciled metav1.Time `json:"lastReconciled,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the secret's state
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

// Condition types for TLSSecret
const (
	// TLSSecretConditionReady indicates the secret is ready and active
	TLSSecretConditionReady = "Ready"
	// TLSSecretConditionReconciled indicates the secret has been successfully reconciled
	TLSSecretConditionReconciled = "Reconciled"
	// TLSSecretConditionError indicates there was an error during reconciliation
	TLSSecretConditionError = "Error"
	// TLSSecretConditionCertExpiring indicates the certificate is expiring soon
	TLSSecretConditionCertExpiring = "CertExpiring" // #nosec G101 -- not a credential, just a condition name
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Active",type="boolean",JSONPath=".status.active",description="Whether the secret is active"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.message",priority=0,description="Status message"
//+kubebuilder:printcolumn:name="Issuer",type="string",JSONPath=".status.certificateInfo.issuer",description="Certificate issuer"
//+kubebuilder:printcolumn:name="Expires",type="string",JSONPath=".status.certificateInfo.notAfter",description="Certificate expiration"
//+kubebuilder:printcolumn:name="Days",type="integer",JSONPath=".status.certificateInfo.daysUntilExpiry",description="Days until expiry"
//+kubebuilder:printcolumn:name="Nodes",type="string",JSONPath=".status.nodes",description="Nodes where secret is deployed"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TLSSecret is the Schema for the tlssecrets API
type TLSSecret struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TLSSecretSpec   `json:"spec,omitempty"`
	Status TLSSecretStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TLSSecretList contains a list of TLSSecret
type TLSSecretList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TLSSecret `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TLSSecret{}, &TLSSecretList{})
}
