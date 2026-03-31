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

package sds

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	xdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types"
)

func TestResolveK8sSecretRef(t *testing.T) {
	const defaultNS = "xds-system"

	tests := []struct {
		name             string
		tlsSecret        *envoyxdsv1alpha1.TLSSecret
		defaultNamespace string
		globalStorage    *xdstypes.StorageConfig
		wantName         string
		wantNamespace    string
	}{
		{
			name: "kubernetes storage with defaults - uses TLSSecret name and namespace",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Config: xdstypes.StorageConfig{
							Type: xdstypes.Kubernetes,
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "my-cert",
			wantNamespace:    "default",
		},
		{
			name: "kubernetes storage with explicit SecretName override",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Config: xdstypes.StorageConfig{
							Type: xdstypes.Kubernetes,
							KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{
								SecretName: "custom-secret-name",
							},
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "custom-secret-name",
			wantNamespace:    "default",
		},
		{
			name: "kubernetes storage with explicit namespace override",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Config: xdstypes.StorageConfig{
							Type: xdstypes.Kubernetes,
							KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{
								Namespace: "other-namespace",
							},
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "my-cert",
			wantNamespace:    "other-namespace",
		},
		{
			name: "kubernetes storage with both name and namespace overrides",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Config: xdstypes.StorageConfig{
							Type: xdstypes.Kubernetes,
							KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{
								SecretName: "custom-name",
								Namespace:  "custom-ns",
							},
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "custom-name",
			wantNamespace:    "custom-ns",
		},
		{
			name: "vault storage returns empty",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Config: xdstypes.StorageConfig{
							Type: xdstypes.Vault,
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "",
			wantNamespace:    "",
		},
		{
			name: "local storage returns empty",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Config: xdstypes.StorageConfig{
							Type: xdstypes.Local,
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "",
			wantNamespace:    "",
		},
		{
			name: "inferred kubernetes - KubernetesStorageConfig present with no explicit type",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Config: xdstypes.StorageConfig{
							KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{
								SecretName: "k8s-secret",
							},
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "k8s-secret",
			wantNamespace:    "default",
		},
		{
			name: "inferred local - explicit local path with no explicit type",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Config: xdstypes.StorageConfig{
							LocalStorageConfig: &xdstypes.LocalStorageConfig{
								Path: "/certs",
							},
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "",
			wantNamespace:    "",
		},
		{
			name: "inferred kubernetes - no config at all defaults to kubernetes",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "my-cert",
			wantNamespace:    "default",
		},
		{
			name: "challenge with vault config returns empty (vault storage)",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Challenge: &xdstypes.ChallengeConfig{
							ChallengeType: xdstypes.DNS01,
						},
						Config: xdstypes.StorageConfig{
							VaultStorageConfig: &xdstypes.VaultStorageConfig{
								Path: "/secret/certs",
							},
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "",
			wantNamespace:    "",
		},
		{
			name: "challenge with global vault config returns empty (vault storage)",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Challenge: &xdstypes.ChallengeConfig{
							ChallengeType: xdstypes.DNS01,
						},
						// No per-resource VaultStorageConfig
					},
				},
			},
			defaultNamespace: defaultNS,
			globalStorage: &xdstypes.StorageConfig{
				VaultStorageConfig: &xdstypes.VaultStorageConfig{
					Path: "/secret/global-certs",
					URL:  "https://vault.example.com",
				},
			},
			wantName:      "",
			wantNamespace: "",
		},
		{
			name: "challenge without vault config defaults to kubernetes",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "le-cert",
					Namespace: "default",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "le-cert",
						Domains:    []string{"example.com"},
						Challenge: &xdstypes.ChallengeConfig{
							ChallengeType: xdstypes.DNS01,
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "le-cert",
			wantNamespace:    "default",
		},
		{
			name: "TLSSecret without namespace falls back to defaultNamespace",
			tlsSecret: &envoyxdsv1alpha1.TLSSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-cert",
				},
				Spec: envoyxdsv1alpha1.TLSSecretSpec{
					DomainConfig: xdstypes.DomainConfig{
						SecretName: "my-cert",
						Domains:    []string{"example.com"},
						Config: xdstypes.StorageConfig{
							Type: xdstypes.Kubernetes,
						},
					},
				},
			},
			defaultNamespace: defaultNS,
			wantName:         "my-cert",
			wantNamespace:    defaultNS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, namespace := resolveK8sSecretRef(tt.tlsSecret, tt.defaultNamespace, tt.globalStorage)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantNamespace, namespace)
		})
	}
}

func TestFindTLSSecretsForK8sSecret(t *testing.T) {
	const defaultNS = "xds-system"

	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name         string
		secret       *corev1.Secret
		tlsSecrets   []envoyxdsv1alpha1.TLSSecret
		wantRequests []reconcile.Request
	}{
		{
			name: "matches TLSSecret by default name (same name as TLSSecret)",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
			},
			tlsSecrets: []envoyxdsv1alpha1.TLSSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cert",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "my-cert",
							Domains:    []string{"example.com"},
							Config: xdstypes.StorageConfig{
								Type: xdstypes.Kubernetes,
							},
						},
					},
				},
			},
			wantRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "my-cert", Namespace: "default"}},
			},
		},
		{
			name: "matches TLSSecret by explicit SecretName in KubernetesStorageConfig",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "custom-k8s-secret",
					Namespace: "default",
				},
			},
			tlsSecrets: []envoyxdsv1alpha1.TLSSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cert",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "my-cert",
							Domains:    []string{"example.com"},
							Config: xdstypes.StorageConfig{
								Type: xdstypes.Kubernetes,
								KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{
									SecretName: "custom-k8s-secret",
								},
							},
						},
					},
				},
			},
			wantRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "my-cert", Namespace: "default"}},
			},
		},
		{
			name: "matches TLSSecret with cross-namespace reference",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shared-cert",
					Namespace: "cert-store",
				},
			},
			tlsSecrets: []envoyxdsv1alpha1.TLSSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cert",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "my-cert",
							Domains:    []string{"example.com"},
							Config: xdstypes.StorageConfig{
								Type: xdstypes.Kubernetes,
								KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{
									SecretName: "shared-cert",
									Namespace:  "cert-store",
								},
							},
						},
					},
				},
			},
			wantRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "my-cert", Namespace: "default"}},
			},
		},
		{
			name: "no match - secret name differs",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-secret",
					Namespace: "default",
				},
			},
			tlsSecrets: []envoyxdsv1alpha1.TLSSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cert",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "my-cert",
							Domains:    []string{"example.com"},
							Config: xdstypes.StorageConfig{
								Type: xdstypes.Kubernetes,
							},
						},
					},
				},
			},
			wantRequests: nil,
		},
		{
			name: "no match - secret namespace differs",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "other-ns",
				},
			},
			tlsSecrets: []envoyxdsv1alpha1.TLSSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cert",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "my-cert",
							Domains:    []string{"example.com"},
							Config: xdstypes.StorageConfig{
								Type: xdstypes.Kubernetes,
							},
						},
					},
				},
			},
			wantRequests: nil,
		},
		{
			name: "vault storage TLSSecret is skipped",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
			},
			tlsSecrets: []envoyxdsv1alpha1.TLSSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cert",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "my-cert",
							Domains:    []string{"example.com"},
							Config: xdstypes.StorageConfig{
								Type: xdstypes.Vault,
							},
						},
					},
				},
			},
			wantRequests: nil,
		},
		{
			name: "multiple TLSSecrets referencing the same K8s Secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shared-tls",
					Namespace: "certs",
				},
			},
			tlsSecrets: []envoyxdsv1alpha1.TLSSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cert-a",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "cert-a",
							Domains:    []string{"a.example.com"},
							Config: xdstypes.StorageConfig{
								Type: xdstypes.Kubernetes,
								KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{
									SecretName: "shared-tls",
									Namespace:  "certs",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cert-b",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "cert-b",
							Domains:    []string{"b.example.com"},
							Config: xdstypes.StorageConfig{
								Type: xdstypes.Kubernetes,
								KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{
									SecretName: "shared-tls",
									Namespace:  "certs",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cert-c",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "cert-c",
							Domains:    []string{"c.example.com"},
							Config: xdstypes.StorageConfig{
								Type: xdstypes.Vault,
							},
						},
					},
				},
			},
			wantRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "cert-a", Namespace: "default"}},
				{NamespacedName: types.NamespacedName{Name: "cert-b", Namespace: "default"}},
			},
		},
		{
			name: "empty TLSSecret list returns nil",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cert",
					Namespace: "default",
				},
			},
			tlsSecrets:   nil,
			wantRequests: nil,
		},
		{
			name: "inferred kubernetes storage - no explicit type set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auto-cert",
					Namespace: "default",
				},
			},
			tlsSecrets: []envoyxdsv1alpha1.TLSSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "auto-cert",
						Namespace: "default",
					},
					Spec: envoyxdsv1alpha1.TLSSecretSpec{
						DomainConfig: xdstypes.DomainConfig{
							SecretName: "auto-cert",
							Domains:    []string{"auto.example.com"},
							// No Config.Type set, no storage configs - should default to Kubernetes
						},
					},
				},
			},
			wantRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "auto-cert", Namespace: "default"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := make([]runtime.Object, 0, len(tt.tlsSecrets))
			for i := range tt.tlsSecrets {
				objs = append(objs, &tt.tlsSecrets[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			r := &TLSSecretReconciler{
				Client: fakeClient,
				Config: &xds.Config{
					DefaultNamespace: defaultNS,
				},
			}

			requests := r.findTLSSecretsForK8sSecret(context.Background(), tt.secret)
			assert.Equal(t, tt.wantRequests, requests)
		})
	}
}
