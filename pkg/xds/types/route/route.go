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

// Package route provides composite types for Route CRs.
// This combines listener binding, HCM configuration, and route configuration.
package route

import (
	"github.com/tentens-tech/xds-controller/pkg/xds/types/hcm"
	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
	"github.com/tentens-tech/xds-controller/pkg/xds/types/rds"
)

// Route is the composite configuration for a Route CR.
// It combines:
// - Listener binding (which listener and filter chain match)
// - HTTP Connection Manager (HCM) configuration (embedded from generated types)
// - Route configuration (Envoy RouteConfiguration)
type Route struct {
	// ListenerRefs specifies the listener(s) this route attaches to.
	// Each entry is a listener name. The route will be added to all listed listeners.
	ListenerRefs []string `json:"listener_refs,omitempty"`

	// TLSSecretRef specifies the TLS secret to use for this route.
	// References a TLSSecret CR by name.
	TLSSecretRef string `json:"tlssecret_ref,omitempty"`

	// FilterChainMatch defines when this route's filter chain is selected.
	// This is dynamically added to the listener's filter chains.
	FilterChainMatch *lds.FilterChainMatch `json:"filter_chain_match,omitempty"`

	// RouteConfig is the inline RouteConfiguration.
	// This uses our generated RDS types for full Envoy RouteConfiguration support.
	RouteConfig *rds.RDS `json:"route_config,omitempty"`

	// HCM embeds all HTTP Connection Manager configuration fields.
	// This includes codec_type, stat_prefix, http_filters, tracing, access_log, etc.
	hcm.HCM `json:",inline"`
}

// DeepCopyInto copies the receiver into out.
func (in *Route) DeepCopyInto(out *Route) {
	*out = *in
	if in.ListenerRefs != nil {
		in, out := &in.ListenerRefs, &out.ListenerRefs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.FilterChainMatch != nil {
		in, out := &in.FilterChainMatch, &out.FilterChainMatch
		*out = new(lds.FilterChainMatch)
		(*in).DeepCopyInto(*out)
	}
	if in.RouteConfig != nil {
		in, out := &in.RouteConfig, &out.RouteConfig
		*out = new(rds.RDS)
		(*in).DeepCopyInto(*out)
	}
	// DeepCopy embedded HCM fields
	in.HCM.DeepCopyInto(&out.HCM)
}
