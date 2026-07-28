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
	"strconv"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Annotation and label keys understood by the TLSSecret controller.
//
// Every key may be set either as an annotation or as a label; the annotation
// takes precedence. Label values are limited to [-A-Za-z0-9_.] by Kubernetes,
// which is compatible with the duration strings accepted here (e.g. "30m",
// "168h", "1h30m").
const (
	// AnnotationForceRenew requests a new certificate on the next reconcile,
	// bypassing the "certificate is still valid" check. The controller removes
	// the annotation only after the new certificate has been stored, so a
	// failed attempt keeps retrying on the backoff schedule.
	AnnotationForceRenew = "envoyxds.io/force-renew"

	// AnnotationPause stops all certificate operations for this TLSSecret.
	// The existing secret stays in the Envoy snapshot untouched. Reconciliation
	// resumes when the annotation is removed.
	AnnotationPause = "envoyxds.io/pause"

	// AnnotationForceRenewedAt records when the last force-renew completed.
	AnnotationForceRenewedAt = "envoyxds.io/force-renewed-at"

	// KeyRetryBaseDelay overrides the delay before the first retry after a failure.
	KeyRetryBaseDelay = "envoyxds.io/retry-base-delay"

	// KeyRetryMaxDelay overrides the upper bound of the exponential backoff.
	KeyRetryMaxDelay = "envoyxds.io/retry-max-delay"

	// KeyRetryMultiplier overrides the growth factor of the exponential backoff.
	KeyRetryMultiplier = "envoyxds.io/retry-multiplier"
)

// Defaults for the retry policy when neither the resource nor the controller
// configuration specifies a value. The resulting schedule is
// 10m, 20m, 40m, 80m, ... capped at one week.
const (
	DefaultRetryBaseDelay  = 10 * time.Minute
	DefaultRetryMaxDelay   = 168 * time.Hour
	DefaultRetryMultiplier = 2.0
)

// RetryPolicy describes the exponential backoff applied between failed
// certificate operations for a single TLSSecret.
type RetryPolicy struct {
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	Multiplier float64
}

// DefaultRetryPolicy returns the built-in backoff schedule.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		BaseDelay:  DefaultRetryBaseDelay,
		MaxDelay:   DefaultRetryMaxDelay,
		Multiplier: DefaultRetryMultiplier,
	}
}

// normalize replaces non-positive or nonsensical values with the defaults so a
// partially filled policy is always usable.
func (p RetryPolicy) normalize() RetryPolicy {
	if p.BaseDelay <= 0 {
		p.BaseDelay = DefaultRetryBaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = DefaultRetryMaxDelay
	}
	if p.Multiplier < 1 {
		p.Multiplier = DefaultRetryMultiplier
	}
	if p.MaxDelay < p.BaseDelay {
		p.MaxDelay = p.BaseDelay
	}
	return p
}

// DelayFor returns the wait before the failureCount-th retry. The first failure
// waits BaseDelay, and each subsequent failure multiplies that by Multiplier up
// to MaxDelay.
func (p RetryPolicy) DelayFor(failureCount int32) time.Duration {
	p = p.normalize()
	if failureCount <= 1 {
		return p.BaseDelay
	}

	// Work in float64 seconds: 2^63 nanoseconds is only ~292 years, and the
	// exponent grows fast enough to overflow an int64 for large failure counts.
	delay := float64(p.BaseDelay/time.Second) * math.Pow(p.Multiplier, float64(failureCount-1))
	maxDelay := float64(p.MaxDelay / time.Second)
	if math.IsInf(delay, 0) || delay >= maxDelay {
		return p.MaxDelay
	}
	return time.Duration(delay) * time.Second
}

// RetryPolicyFor resolves the retry policy for a TLSSecret. Values are read
// from the object's annotations first, then its labels, then fall back to the
// supplied controller-wide defaults. Unparsable values are ignored so a typo
// degrades to the default instead of breaking reconciliation.
func RetryPolicyFor(obj client.Object, defaults RetryPolicy) RetryPolicy {
	policy := defaults.normalize()

	if d, ok := metaDuration(obj, KeyRetryBaseDelay); ok {
		policy.BaseDelay = d
	}
	if d, ok := metaDuration(obj, KeyRetryMaxDelay); ok {
		policy.MaxDelay = d
	}
	if v, ok := metaValue(obj, KeyRetryMultiplier); ok {
		if m, err := strconv.ParseFloat(v, 64); err == nil && m >= 1 {
			policy.Multiplier = m
		}
	}

	return policy.normalize()
}

// IsPaused reports whether certificate operations are suspended for this object.
// The annotation only has to be present; an explicit falsey value disables it
// without having to remove the key.
func IsPaused(obj client.Object) bool {
	return flagSet(obj, AnnotationPause)
}

// IsForceRenew reports whether a new certificate has been explicitly requested.
func IsForceRenew(obj client.Object) bool {
	return flagSet(obj, AnnotationForceRenew)
}

// ForceRenewRequest returns the raw value of the force-renew annotation. The
// controller records it in status so that changing the value (for example to a
// fresh timestamp) is recognized as a new request and clears any pending
// backoff, while a stale annotation keeps waiting out the current backoff.
func ForceRenewRequest(obj client.Object) string {
	if !IsForceRenew(obj) {
		return ""
	}
	v, _ := metaValue(obj, AnnotationForceRenew)
	if v == "" {
		// Distinguish "annotation present with empty value" from "absent".
		return "true"
	}
	return v
}

// flagSet treats the presence of an annotation as "on" unless its value parses
// as a false boolean, so both `kubectl annotate ... pause=""` and
// `... pause=true` enable it.
func flagSet(obj client.Object, key string) bool {
	v, ok := metaValue(obj, key)
	if !ok {
		return false
	}
	if v == "" {
		return true
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	// Any other non-empty value (e.g. a timestamp used as a nonce) means "on".
	return true
}

// metaValue looks up a key in the object's annotations, then its labels.
func metaValue(obj client.Object, key string) (string, bool) {
	if obj == nil {
		return "", false
	}
	if v, ok := obj.GetAnnotations()[key]; ok {
		return v, true
	}
	if v, ok := obj.GetLabels()[key]; ok {
		return v, true
	}
	return "", false
}

// metaDuration looks up a key and parses it as a Go duration.
func metaDuration(obj client.Object, key string) (time.Duration, bool) {
	v, ok := metaValue(obj, key)
	if !ok || v == "" {
		return 0, false
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}
