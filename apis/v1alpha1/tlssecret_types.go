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

	// FailureCount is the number of consecutive failed certificate operations.
	// It is reset to zero after a successful reconciliation and drives the
	// exponential backoff between retries.
	// +optional
	FailureCount int32 `json:"failureCount,omitempty"`

	// LastFailureTime is when the most recent certificate operation failed
	// +optional
	LastFailureTime *metav1.Time `json:"lastFailureTime,omitempty"`

	// BackoffUntil is the earliest time the next certificate operation will be
	// attempted. Persisted so that a controller restart does not reset the
	// backoff and re-trigger upstream rate limiting.
	// +optional
	BackoffUntil *metav1.Time `json:"backoffUntil,omitempty"`

	// NextRetryDelay is the human readable backoff currently in effect
	// +optional
	NextRetryDelay string `json:"nextRetryDelay,omitempty"`

	// Paused indicates certificate operations are suspended by the
	// envoyxds.io/pause annotation
	// +optional
	Paused bool `json:"paused,omitempty"`

	// ForceRenewRequest is the value of the envoyxds.io/force-renew annotation
	// the controller has already acted on. A different value means a new
	// request, which clears any pending backoff so the retry happens at once.
	// +optional
	ForceRenewRequest string `json:"forceRenewRequest,omitempty"`
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
	// TLSSecretConditionPaused indicates certificate operations are suspended
	TLSSecretConditionPaused = "Paused"
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
//+kubebuilder:printcolumn:name="Retry-In",type="string",JSONPath=".status.nextRetryDelay",priority=1,description="Backoff before the next retry after a failure"
//+kubebuilder:printcolumn:name="Failures",type="integer",JSONPath=".status.failureCount",priority=1,description="Consecutive failed certificate operations"
//+kubebuilder:printcolumn:name="Paused",type="boolean",JSONPath=".status.paused",priority=1,description="Whether certificate operations are suspended"

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
