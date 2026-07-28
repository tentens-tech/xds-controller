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
	"fmt"
	"testing"
	"time"

	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	xdserr "github.com/tentens-tech/xds-controller/pkg/xds/err"
	xdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types"
)

func TestRemoveSecretByName(t *testing.T) {
	t.Run("removes every copy under a node", func(t *testing.T) {
		configs := map[string][]*auth.Secret{
			"node-a": {{Name: "keep-1"}, {Name: "drop"}, {Name: "keep-2"}, {Name: "drop"}},
		}

		removed := removeSecretByName(configs, "node-a", "drop")

		assert.Equal(t, 2, removed)
		require.Len(t, configs["node-a"], 2)
		assert.Equal(t, "keep-1", configs["node-a"][0].Name)
		assert.Equal(t, "keep-2", configs["node-a"][1].Name)
	})

	t.Run("adjacent duplicates are not skipped", func(t *testing.T) {
		// The previous implementation deleted while ranging over the same
		// slice, so the element shifted into the freed index was never visited.
		configs := map[string][]*auth.Secret{
			"node-a": {{Name: "drop"}, {Name: "drop"}, {Name: "drop"}},
		}

		removed := removeSecretByName(configs, "node-a", "drop")

		assert.Equal(t, 3, removed)
		assert.Empty(t, configs["node-a"])
	})

	t.Run("no match leaves the slice untouched", func(t *testing.T) {
		configs := map[string][]*auth.Secret{"node-a": {{Name: "keep"}}}

		removed := removeSecretByName(configs, "node-a", "missing")

		assert.Equal(t, 0, removed)
		require.Len(t, configs["node-a"], 1)
	})

	t.Run("unknown node is a no-op", func(t *testing.T) {
		configs := map[string][]*auth.Secret{}
		assert.Equal(t, 0, removeSecretByName(configs, "nope", "drop"))
	})
}

func TestRemoveSecretFromConfigsAcrossNodes(t *testing.T) {
	r := &TLSSecretReconciler{
		Config: &xds.Config{
			SecretConfigs: map[string][]*auth.Secret{
				"node-a": {{Name: "my-cert"}, {Name: "other"}},
				"node-b": {{Name: "my-cert"}},
				"node-c": {{Name: "unrelated"}},
			},
		},
	}

	r.removeSecretFromConfigs(ctrl.Log, "my-cert")

	assert.Equal(t, []*auth.Secret{{Name: "other"}}, r.Config.SecretConfigs["node-a"])
	assert.Empty(t, r.Config.SecretConfigs["node-b"])
	assert.Len(t, r.Config.SecretConfigs["node-c"], 1)
	assert.Equal(t, uint64(2), r.Config.GetConfigCounter())
}

func TestFormatRenewIn(t *testing.T) {
	tests := []struct {
		renewIn time.Duration
		want    string
	}{
		{0, "0d 0h 0m"},
		{-90 * time.Minute, "0d 0h 0m"}, // never render a negative countdown
		{30 * time.Minute, "0d 0h 30m"},
		{25 * time.Hour, "1d 1h 0m"},
		{60*24*time.Hour + 3*time.Hour + 15*time.Minute, "60d 3h 15m"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, formatRenewIn(tt.renewIn))
		})
	}
}

func TestRequeueInterval(t *testing.T) {
	r := &TLSSecretReconciler{Config: &xds.Config{StatusRefreshInterval: time.Hour}}

	// A renewal two months out must still be revisited within the refresh
	// interval, otherwise the status fields go stale for the whole period.
	assert.Equal(t, time.Hour, r.requeueInterval(60*24*time.Hour))
	assert.Equal(t, 20*time.Minute, r.requeueInterval(20*time.Minute))
	assert.Equal(t, 30*time.Second, r.requeueInterval(0))
	assert.Equal(t, 30*time.Second, r.requeueInterval(-time.Hour))

	unset := &TLSSecretReconciler{Config: &xds.Config{}}
	assert.Equal(t, time.Hour, unset.requeueInterval(60*24*time.Hour))
}

func TestStatusHeartbeat(t *testing.T) {
	// The heartbeat tracks the reconcile cadence and sits just under it, so a
	// scheduled wake-up always finds the timestamp due for a refresh.
	hourly := &TLSSecretReconciler{Config: &xds.Config{StatusRefreshInterval: time.Hour}}
	assert.Equal(t, 54*time.Minute, hourly.statusHeartbeat())
	assert.Less(t, hourly.statusHeartbeat(), hourly.requeueInterval(60*24*time.Hour))

	fast := &TLSSecretReconciler{Config: &xds.Config{StatusRefreshInterval: 20 * time.Second}}
	assert.Equal(t, 18*time.Second, fast.statusHeartbeat())

	tiny := &TLSSecretReconciler{Config: &xds.Config{StatusRefreshInterval: time.Second}}
	assert.Equal(t, minStatusHeartbeat, tiny.statusHeartbeat())

	unset := &TLSSecretReconciler{Config: &xds.Config{}}
	assert.Equal(t, 54*time.Minute, unset.statusHeartbeat())
}

func TestCertificateChanged(t *testing.T) {
	a := &envoyxdsv1alpha1.CertificateInfo{Fingerprint: "aaa"}
	b := &envoyxdsv1alpha1.CertificateInfo{Fingerprint: "bbb"}

	assert.True(t, certificateChanged(nil, a), "first certificate counts as a change")
	assert.True(t, certificateChanged(a, b))
	assert.False(t, certificateChanged(a, a))
	assert.False(t, certificateChanged(a, nil), "no new certificate is not a change")
}

func TestErrorClass(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"rate limited", xdserr.ErrRateLimited, "rate limited"},
		{"wrapped rate limited", fmt.Errorf("obtain: %w", xdserr.ErrRateLimited), "rate limited"},
		{"service busy", xdserr.ErrServiceBusy, "service busy"},
		{"cert not found", xdserr.ErrCertNotFound, "cert not found"},
		{"vault", xdserr.ErrVaultNotConfigured, "vault not configured"},
		{"get cert", fmt.Errorf(xdserr.ErrGetCert.Error()+": %w", assertErr("boom")), "get cert"},
		{"unknown", assertErr("something else"), "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorClass(tt.err)
			assert.Equal(t, tt.want, got)
			// The result is used as a Prometheus label, so it must be one of a
			// small fixed set rather than the raw error text.
			assert.NotContains(t, got, "Next try")
		})
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestTLSSecretStatusEqual(t *testing.T) {
	base := envoyxdsv1alpha1.TLSSecretStatus{
		Active:      true,
		NextRenewal: "30d 0h 0m",
		Nodes:       "edge",
		Clusters:    "prod",
		CertificateInfo: &envoyxdsv1alpha1.CertificateInfo{
			Issuer:          "Let's Encrypt",
			DaysUntilExpiry: 60,
			Fingerprint:     "aaa",
		},
		LastReconciled: metav1.NewTime(time.Now().Add(-time.Hour)),
	}

	t.Run("ignores timestamps that move every reconcile", func(t *testing.T) {
		other := *base.DeepCopy()
		other.LastReconciled = metav1.Now()
		other.Snapshots = nil
		assert.True(t, tlsSecretStatusEqual(base, other))
	})

	t.Run("detects a changed nextRenewal", func(t *testing.T) {
		other := *base.DeepCopy()
		other.NextRenewal = "29d 23h 0m"
		assert.False(t, tlsSecretStatusEqual(base, other),
			"a changed renewal countdown must be written, otherwise the field goes stale")
	})

	t.Run("detects a changed message", func(t *testing.T) {
		other := *base.DeepCopy()
		other.Message = "rate limited"
		assert.False(t, tlsSecretStatusEqual(base, other))
	})

	t.Run("detects a changed condition", func(t *testing.T) {
		other := *base.DeepCopy()
		other.Conditions = []metav1.Condition{{
			Type:   envoyxdsv1alpha1.TLSSecretConditionError,
			Status: metav1.ConditionTrue,
			Reason: "Error",
		}}
		assert.False(t, tlsSecretStatusEqual(base, other))
	})

	t.Run("detects a changed expiry date", func(t *testing.T) {
		other := *base.DeepCopy()
		other.CertificateInfo.NotAfter = metav1.Now()
		assert.False(t, tlsSecretStatusEqual(base, other))
	})

	t.Run("detects a changed failure count", func(t *testing.T) {
		other := *base.DeepCopy()
		other.FailureCount = 3
		assert.False(t, tlsSecretStatusEqual(base, other))
	})

	t.Run("ignores the exact backoff instant", func(t *testing.T) {
		a := *base.DeepCopy()
		b := *base.DeepCopy()
		a.BackoffUntil = ptrTime(metav1.NewTime(time.Now().Add(time.Hour)))
		b.BackoffUntil = ptrTime(metav1.NewTime(time.Now().Add(time.Hour).Add(time.Second)))
		assert.True(t, tlsSecretStatusEqual(a, b))
	})
}

func ptrTime(t metav1.Time) *metav1.Time { return &t }

// newTestReconciler builds a reconciler backed by a fake client. The instance is
// deliberately not the leader, so certificate generation is never attempted and
// the tests exercise the pause and backoff logic in isolation.
func newTestReconciler(t *testing.T, objs ...client.Object) *TLSSecretReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, envoyxdsv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&envoyxdsv1alpha1.TLSSecret{}).
		Build()

	cfg := &xds.Config{
		NodeID:                "edge",
		Cluster:               "prod",
		DefaultNamespace:      "default",
		K8sClient:             fakeClient,
		SecretConfigs:         map[string][]*auth.Secret{},
		ReconciliationStatus:  status.NewReconciliationStatus(),
		StatusRefreshInterval: time.Hour,
		RetryBaseDelay:        10 * time.Minute,
		RetryMaxDelay:         168 * time.Hour,
		RetryMultiplier:       2,
	}

	return &TLSSecretReconciler{Client: fakeClient, Scheme: scheme, Config: cfg}
}

func testTLSSecret(annotations map[string]string) *envoyxdsv1alpha1.TLSSecret {
	return &envoyxdsv1alpha1.TLSSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-cert",
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: envoyxdsv1alpha1.TLSSecretSpec{
			DomainConfig: xdstypes.DomainConfig{
				SecretName: "my-cert",
				Domains:    []string{"example.com"},
				Config:     xdstypes.StorageConfig{Type: xdstypes.Kubernetes},
			},
		},
	}
}

var testRequest = ctrl.Request{
	NamespacedName: types.NamespacedName{Name: "my-cert", Namespace: "default"},
}

func TestReconcilePaused(t *testing.T) {
	tlsSecret := testTLSSecret(map[string]string{AnnotationPause: "true"})
	r := newTestReconciler(t, tlsSecret)

	result, err := r.Reconcile(context.Background(), testRequest)
	require.NoError(t, err)

	// No requeue: the annotation-change predicate wakes the controller when the
	// pause is lifted.
	assert.Zero(t, result.RequeueAfter)

	var got envoyxdsv1alpha1.TLSSecret
	require.NoError(t, r.Get(context.Background(), testRequest.NamespacedName, &got))
	assert.True(t, got.Status.Paused)
	assert.Contains(t, got.Status.Message, AnnotationPause)

	var paused *metav1.Condition
	for i := range got.Status.Conditions {
		if got.Status.Conditions[i].Type == envoyxdsv1alpha1.TLSSecretConditionPaused {
			paused = &got.Status.Conditions[i]
		}
	}
	require.NotNil(t, paused, "Paused condition must be reported")
	assert.Equal(t, metav1.ConditionTrue, paused.Status)
}

func TestReconcilePausedKeepsBackoffState(t *testing.T) {
	tlsSecret := testTLSSecret(map[string]string{AnnotationPause: "true"})
	tlsSecret.Status = envoyxdsv1alpha1.TLSSecretStatus{
		FailureCount:   4,
		NextRetryDelay: "1h20m0s",
		BackoffUntil:   ptrTime(metav1.NewTime(time.Now().Add(time.Hour))),
	}
	r := newTestReconciler(t, tlsSecret)

	_, err := r.Reconcile(context.Background(), testRequest)
	require.NoError(t, err)

	var got envoyxdsv1alpha1.TLSSecret
	require.NoError(t, r.Get(context.Background(), testRequest.NamespacedName, &got))
	// Pausing must not reset the backoff, or pausing and unpausing a
	// rate-limited secret would restart the retries immediately.
	assert.EqualValues(t, 4, got.Status.FailureCount)
	require.NotNil(t, got.Status.BackoffUntil)
}

func TestReconcileWaitsOutBackoff(t *testing.T) {
	tlsSecret := testTLSSecret(nil)
	tlsSecret.Status = envoyxdsv1alpha1.TLSSecretStatus{
		FailureCount: 3,
		BackoffUntil: ptrTime(metav1.NewTime(time.Now().Add(40 * time.Minute))),
	}
	r := newTestReconciler(t, tlsSecret)

	result, err := r.Reconcile(context.Background(), testRequest)
	require.NoError(t, err)

	assert.Greater(t, result.RequeueAfter, 35*time.Minute)
	assert.LessOrEqual(t, result.RequeueAfter, 40*time.Minute)
}

func TestReconcileExpiredBackoffRetries(t *testing.T) {
	tlsSecret := testTLSSecret(nil)
	tlsSecret.Status = envoyxdsv1alpha1.TLSSecretStatus{
		FailureCount: 3,
		BackoffUntil: ptrTime(metav1.NewTime(time.Now().Add(-time.Minute))),
	}
	r := newTestReconciler(t, tlsSecret)

	result, err := r.Reconcile(context.Background(), testRequest)
	require.NoError(t, err)

	// The window has passed, so the reconcile proceeds and (as a non-leader
	// without a stored certificate) requeues on the normal schedule instead of
	// the 40 minute backoff.
	assert.Less(t, result.RequeueAfter, 35*time.Minute)
}

func TestReconcileNewForceRenewBypassesBackoff(t *testing.T) {
	tlsSecret := testTLSSecret(map[string]string{AnnotationForceRenew: "1753697226"})
	tlsSecret.Status = envoyxdsv1alpha1.TLSSecretStatus{
		FailureCount:      3,
		BackoffUntil:      ptrTime(metav1.NewTime(time.Now().Add(40 * time.Minute))),
		ForceRenewRequest: "",
	}
	r := newTestReconciler(t, tlsSecret)

	result, err := r.Reconcile(context.Background(), testRequest)
	require.NoError(t, err)

	assert.Less(t, result.RequeueAfter, 35*time.Minute,
		"a new force-renew request must not wait out the pending backoff")
}

// failingTLSSecret asks for Kubernetes storage; paired with newFailingReconciler
// (which leaves Config.K8sClient unset) GetSecret fails deterministically
// without touching the network.
func failingTLSSecret(annotations map[string]string) *envoyxdsv1alpha1.TLSSecret {
	tlsSecret := testTLSSecret(annotations)
	tlsSecret.Spec.Config = xdstypes.StorageConfig{
		Type:                    xdstypes.Kubernetes,
		KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{Namespace: "default", SecretName: "my-cert"},
	}
	return tlsSecret
}

// newFailingReconciler returns a leader reconciler whose certificate storage is
// unavailable, so every reconcile takes the failure path.
func newFailingReconciler(t *testing.T, objs ...client.Object) *TLSSecretReconciler {
	t.Helper()
	r := newTestReconciler(t, objs...)
	r.Config.SetLeaderStatus(true)
	r.Config.K8sClient = nil
	return r
}

func TestReconcileFailureEscalatesBackoff(t *testing.T) {
	tlsSecret := failingTLSSecret(nil)
	r := newFailingReconciler(t, tlsSecret)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, testRequest)
	require.NoError(t, err, "a failed certificate operation is reported through status, not by failing the reconcile")
	assert.Equal(t, 10*time.Minute, result.RequeueAfter)

	var got envoyxdsv1alpha1.TLSSecret
	require.NoError(t, r.Get(ctx, testRequest.NamespacedName, &got))
	assert.EqualValues(t, 1, got.Status.FailureCount)
	assert.Equal(t, "10m0s", got.Status.NextRetryDelay)
	require.NotNil(t, got.Status.BackoffUntil)
	require.NotNil(t, got.Status.LastFailureTime)
	assert.Contains(t, got.Status.Message, "kubernetes client not configured")

	// Expire the window so the next reconcile actually retries.
	got.Status.BackoffUntil = ptrTime(metav1.NewTime(time.Now().Add(-time.Second)))
	require.NoError(t, r.Status().Update(ctx, &got))

	result, err = r.Reconcile(ctx, testRequest)
	require.NoError(t, err)
	assert.Equal(t, 20*time.Minute, result.RequeueAfter, "each failure doubles the wait")

	require.NoError(t, r.Get(ctx, testRequest.NamespacedName, &got))
	assert.EqualValues(t, 2, got.Status.FailureCount)
	assert.Equal(t, "20m0s", got.Status.NextRetryDelay)
}

func TestReconcileFailureHonorsPerSecretRetryLabel(t *testing.T) {
	tlsSecret := failingTLSSecret(nil)
	tlsSecret.Labels = map[string]string{KeyRetryBaseDelay: "45m"}
	r := newFailingReconciler(t, tlsSecret)

	result, err := r.Reconcile(context.Background(), testRequest)
	require.NoError(t, err)
	assert.Equal(t, 45*time.Minute, result.RequeueAfter)
}

func TestReconcileFailureKeepsPreviousCertificateInfo(t *testing.T) {
	tlsSecret := failingTLSSecret(nil)
	r := newFailingReconciler(t, tlsSecret)
	ctx := context.Background()

	// Seed the state a previous successful reconcile would have written.
	var seeded envoyxdsv1alpha1.TLSSecret
	require.NoError(t, r.Get(ctx, testRequest.NamespacedName, &seeded))
	seeded.Status = envoyxdsv1alpha1.TLSSecretStatus{
		Active:      true,
		Nodes:       "edge",
		Clusters:    "prod",
		NextRenewal: "42d 0h 0m",
		CertificateInfo: &envoyxdsv1alpha1.CertificateInfo{
			Issuer:          "Let's Encrypt",
			DaysUntilExpiry: 42,
			Fingerprint:     "aaa",
		},
		Snapshots: []envoyxdsv1alpha1.SnapshotInfo{{NodeID: "edge", Cluster: "prod", Active: true}},
	}
	require.NoError(t, r.Status().Update(ctx, &seeded))

	_, err := r.Reconcile(ctx, testRequest)
	require.NoError(t, err)

	var got envoyxdsv1alpha1.TLSSecret
	require.NoError(t, r.Get(ctx, testRequest.NamespacedName, &got))

	// Envoy is still serving the previously issued certificate, so the reported
	// issuer, expiry and nodes must survive a failed renewal attempt.
	require.NotNil(t, got.Status.CertificateInfo, "a failed renewal must not blank the certificate details")
	assert.Equal(t, "Let's Encrypt", got.Status.CertificateInfo.Issuer)
	assert.Equal(t, 42, got.Status.CertificateInfo.DaysUntilExpiry)
	assert.Equal(t, "edge", got.Status.Nodes)
	assert.Equal(t, "prod", got.Status.Clusters)
	assert.Equal(t, "42d 0h 0m", got.Status.NextRenewal)
	assert.True(t, got.Status.Active)

	// ...while still reporting the failure.
	assert.EqualValues(t, 1, got.Status.FailureCount)
	var errCond *metav1.Condition
	for i := range got.Status.Conditions {
		if got.Status.Conditions[i].Type == envoyxdsv1alpha1.TLSSecretConditionError {
			errCond = &got.Status.Conditions[i]
		}
	}
	require.NotNil(t, errCond)
	assert.Equal(t, metav1.ConditionTrue, errCond.Status)
}

func TestReconcileForceRenewIssuesNewCertificateAndClearsAnnotation(t *testing.T) {
	tlsSecret := testTLSSecret(nil)
	tlsSecret.Spec.Config = xdstypes.StorageConfig{
		Type:                    xdstypes.Kubernetes,
		KubernetesStorageConfig: &xdstypes.KubernetesStorageConfig{Namespace: "default", SecretName: "my-cert"},
	}
	r := newTestReconciler(t, tlsSecret)
	r.Config.SetLeaderStatus(true)
	ctx := context.Background()

	// First pass issues a self-signed certificate and records it.
	_, err := r.Reconcile(ctx, testRequest)
	require.NoError(t, err)

	var issued envoyxdsv1alpha1.TLSSecret
	require.NoError(t, r.Get(ctx, testRequest.NamespacedName, &issued))
	require.NotNil(t, issued.Status.CertificateInfo)
	first := issued.Status.CertificateInfo.Fingerprint
	require.NotEmpty(t, first)

	// A second pass on its own must not re-issue: the certificate is still valid.
	_, err = r.Reconcile(ctx, testRequest)
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, testRequest.NamespacedName, &issued))
	assert.Equal(t, first, issued.Status.CertificateInfo.Fingerprint)

	// Ask for a renewal.
	issued.Annotations[AnnotationForceRenew] = "true"
	require.NoError(t, r.Update(ctx, &issued))

	_, err = r.Reconcile(ctx, testRequest)
	require.NoError(t, err)

	var renewed envoyxdsv1alpha1.TLSSecret
	require.NoError(t, r.Get(ctx, testRequest.NamespacedName, &renewed))
	require.NotNil(t, renewed.Status.CertificateInfo)
	assert.NotEqual(t, first, renewed.Status.CertificateInfo.Fingerprint,
		"force-renew must produce a different certificate")
	assert.NotContains(t, renewed.Annotations, AnnotationForceRenew,
		"the annotation is cleared once the new certificate is stored")
	assert.Contains(t, renewed.Annotations, AnnotationForceRenewedAt)
	assert.EqualValues(t, 0, renewed.Status.FailureCount)
}

func TestReconcileForceRenewKeptOnFailure(t *testing.T) {
	tlsSecret := failingTLSSecret(map[string]string{AnnotationForceRenew: "true"})
	r := newFailingReconciler(t, tlsSecret)
	ctx := context.Background()

	_, err := r.Reconcile(ctx, testRequest)
	require.NoError(t, err)

	var got envoyxdsv1alpha1.TLSSecret
	require.NoError(t, r.Get(ctx, testRequest.NamespacedName, &got))
	assert.Contains(t, got.Annotations, AnnotationForceRenew,
		"a failed force-renew keeps the annotation so the request is not silently dropped")
	assert.EqualValues(t, 1, got.Status.FailureCount)
	assert.Equal(t, "true", got.Status.ForceRenewRequest,
		"the handled request value is recorded so the retry honors the backoff")
}

func TestReconcileStaleForceRenewRespectsBackoff(t *testing.T) {
	tlsSecret := testTLSSecret(map[string]string{AnnotationForceRenew: "1753697226"})
	tlsSecret.Status = envoyxdsv1alpha1.TLSSecretStatus{
		FailureCount:      3,
		BackoffUntil:      ptrTime(metav1.NewTime(time.Now().Add(40 * time.Minute))),
		ForceRenewRequest: "1753697226",
	}
	r := newTestReconciler(t, tlsSecret)

	result, err := r.Reconcile(context.Background(), testRequest)
	require.NoError(t, err)

	assert.Greater(t, result.RequeueAfter, 35*time.Minute,
		"an already handled force-renew request must not retry on every watch event")
}
