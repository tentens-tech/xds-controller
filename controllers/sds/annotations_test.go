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
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
)

func tlsSecretWithMeta(annotations, labels map[string]string) *envoyxdsv1alpha1.TLSSecret {
	return &envoyxdsv1alpha1.TLSSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-cert",
			Namespace:   "default",
			Annotations: annotations,
			Labels:      labels,
		},
	}
}

func TestRetryPolicyDelayFor(t *testing.T) {
	tests := []struct {
		name         string
		policy       RetryPolicy
		failureCount int32
		want         time.Duration
	}{
		{"first failure waits the base delay", DefaultRetryPolicy(), 1, 10 * time.Minute},
		{"second failure doubles", DefaultRetryPolicy(), 2, 20 * time.Minute},
		{"third failure doubles again", DefaultRetryPolicy(), 3, 40 * time.Minute},
		{"fourth failure doubles again", DefaultRetryPolicy(), 4, 80 * time.Minute},
		{"fifth failure doubles again", DefaultRetryPolicy(), 5, 160 * time.Minute},
		{"caps at one week", DefaultRetryPolicy(), 12, 168 * time.Hour},
		{"stays capped for absurd failure counts", DefaultRetryPolicy(), 5000, 168 * time.Hour},
		{"zero failures is treated as the first", DefaultRetryPolicy(), 0, 10 * time.Minute},
		{"negative failures is treated as the first", DefaultRetryPolicy(), -3, 10 * time.Minute},
		{
			// Integer-second arithmetic used to truncate this to zero, and a
			// zero RequeueAfter means "do not requeue" - retries stopped.
			name:         "sub-second base delay is preserved",
			policy:       RetryPolicy{BaseDelay: 500 * time.Millisecond, MaxDelay: time.Hour, Multiplier: 2},
			failureCount: 2,
			want:         time.Second,
		},
		{
			name:         "sub-second base delay keeps doubling",
			policy:       RetryPolicy{BaseDelay: 500 * time.Millisecond, MaxDelay: time.Hour, Multiplier: 2},
			failureCount: 4,
			want:         4 * time.Second,
		},
		{
			name:         "fractional multiplier keeps precision",
			policy:       RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, Multiplier: 1.5},
			failureCount: 3,
			want:         135 * time.Second,
		},
		{
			// NaN fails every comparison, so it used to slip past the `< 1`
			// guard and yield a zero duration.
			name:         "NaN multiplier falls back to the default",
			policy:       RetryPolicy{BaseDelay: 10 * time.Minute, MaxDelay: 168 * time.Hour, Multiplier: math.NaN()},
			failureCount: 3,
			want:         40 * time.Minute,
		},
		{
			name:         "infinite multiplier falls back to the default",
			policy:       RetryPolicy{BaseDelay: 10 * time.Minute, MaxDelay: 168 * time.Hour, Multiplier: math.Inf(1)},
			failureCount: 3,
			want:         40 * time.Minute,
		},
		{
			name:         "custom base and cap",
			policy:       RetryPolicy{BaseDelay: 30 * time.Minute, MaxDelay: 2 * time.Hour, Multiplier: 2},
			failureCount: 3,
			want:         2 * time.Hour,
		},
		{
			name:         "custom multiplier",
			policy:       RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, Multiplier: 3},
			failureCount: 3,
			want:         9 * time.Minute,
		},
		{
			name:         "zero values fall back to the defaults",
			policy:       RetryPolicy{},
			failureCount: 2,
			want:         20 * time.Minute,
		},
		{
			name:         "cap below base is raised to the base",
			policy:       RetryPolicy{BaseDelay: time.Hour, MaxDelay: time.Minute, Multiplier: 2},
			failureCount: 1,
			want:         time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.policy.DelayFor(tt.failureCount))
		})
	}
}

func TestRetryPolicyFor(t *testing.T) {
	defaults := DefaultRetryPolicy()

	tests := []struct {
		name        string
		annotations map[string]string
		labels      map[string]string
		want        RetryPolicy
	}{
		{
			name: "no overrides uses the defaults",
			want: defaults,
		},
		{
			name:   "labels are honored",
			labels: map[string]string{KeyRetryBaseDelay: "30m", KeyRetryMaxDelay: "24h"},
			want:   RetryPolicy{BaseDelay: 30 * time.Minute, MaxDelay: 24 * time.Hour, Multiplier: 2},
		},
		{
			name:        "annotations are honored",
			annotations: map[string]string{KeyRetryBaseDelay: "1h30m", KeyRetryMultiplier: "3"},
			want:        RetryPolicy{BaseDelay: 90 * time.Minute, MaxDelay: DefaultRetryMaxDelay, Multiplier: 3},
		},
		{
			name:        "annotation wins over label",
			annotations: map[string]string{KeyRetryBaseDelay: "5m"},
			labels:      map[string]string{KeyRetryBaseDelay: "45m"},
			want:        RetryPolicy{BaseDelay: 5 * time.Minute, MaxDelay: DefaultRetryMaxDelay, Multiplier: 2},
		},
		{
			name:        "unparsable values fall back to the defaults",
			annotations: map[string]string{KeyRetryBaseDelay: "soon", KeyRetryMaxDelay: "", KeyRetryMultiplier: "lots"},
			want:        defaults,
		},
		{
			name:        "non-positive durations are ignored",
			annotations: map[string]string{KeyRetryBaseDelay: "0s", KeyRetryMaxDelay: "-1h"},
			want:        defaults,
		},
		{
			name:        "multiplier below one is ignored",
			annotations: map[string]string{KeyRetryMultiplier: "0.5"},
			want:        defaults,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RetryPolicyFor(tlsSecretWithMeta(tt.annotations, tt.labels), defaults)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsPausedAndForceRenew(t *testing.T) {
	tests := []struct {
		name           string
		annotations    map[string]string
		labels         map[string]string
		wantPaused     bool
		wantForceRenew bool
		wantRenewValue string
	}{
		{
			name: "no annotations",
		},
		{
			name:        "pause true",
			annotations: map[string]string{AnnotationPause: "true"},
			wantPaused:  true,
		},
		{
			name:        "pause with empty value still pauses",
			annotations: map[string]string{AnnotationPause: ""},
			wantPaused:  true,
		},
		{
			name:        "pause explicitly disabled",
			annotations: map[string]string{AnnotationPause: "false"},
			wantPaused:  false,
		},
		{
			name:           "force renew with an arbitrary value",
			annotations:    map[string]string{AnnotationForceRenew: "1753697226"},
			wantForceRenew: true,
			wantRenewValue: "1753697226",
		},
		{
			name:           "force renew with an empty value normalizes to true",
			annotations:    map[string]string{AnnotationForceRenew: ""},
			wantForceRenew: true,
			wantRenewValue: "true",
		},
		{
			name:           "force renew explicitly disabled",
			annotations:    map[string]string{AnnotationForceRenew: "0"},
			wantForceRenew: false,
			wantRenewValue: "",
		},
		{
			// Labels are set in bulk by selectors and sync tooling, so they
			// must not be able to suspend certificate management.
			name:       "pause label does not pause",
			labels:     map[string]string{AnnotationPause: "true"},
			wantPaused: false,
		},
		{
			// Likewise a label must never be able to trigger an ACME order.
			name:           "force-renew label does not renew",
			labels:         map[string]string{AnnotationForceRenew: "true"},
			wantForceRenew: false,
			wantRenewValue: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := tlsSecretWithMeta(tt.annotations, tt.labels)
			assert.Equal(t, tt.wantPaused, IsPaused(obj), "IsPaused")
			assert.Equal(t, tt.wantForceRenew, IsForceRenew(obj), "IsForceRenew")
			assert.Equal(t, tt.wantRenewValue, ForceRenewRequest(obj), "ForceRenewRequest")
		})
	}
}
