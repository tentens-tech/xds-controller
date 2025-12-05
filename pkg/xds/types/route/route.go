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
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
	"github.com/tentens-tech/xds-controller/pkg/xds/types/rds"
)

// Route is the composite configuration for a Route CR.
// It combines:
// - Listener binding (which listener and filter chain match)
// - HTTP Connection Manager (HCM) configuration
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

	// CodecType supplies the type of codec the connection manager should use.
	// Valid values: AUTO, HTTP1, HTTP2, HTTP3
	CodecType string `json:"codec_type,omitempty"`

	// StatPrefix is the human readable prefix for statistics.
	StatPrefix string `json:"stat_prefix,omitempty"`

	// GenerateRequestId whether to generate x-request-id header if not present.
	GenerateRequestId *bool `json:"generate_request_id,omitempty"`

	// RouteConfig is the inline RouteConfiguration.
	RouteConfig *rds.RDS `json:"route_config,omitempty"`

	// RDS configures dynamic route discovery via RDS API.
	RDS *RDSConfig `json:"rds,omitempty"`

	// HttpFilters is a list of HTTP filters for the connection manager.
	// +kubebuilder:pruning:PreserveUnknownFields
	HttpFilters []*HTTPFilter `json:"http_filters,omitempty"`

	// UseRemoteAddress controls if the connection manager uses the real remote address.
	UseRemoteAddress *bool `json:"use_remote_address,omitempty"`

	// XffNumTrustedHops specifies the number of trusted proxies in XFF header.
	XffNumTrustedHops uint32 `json:"xff_num_trusted_hops,omitempty"`

	// UpgradeConfigs configures protocol upgrades (e.g., WebSocket).
	UpgradeConfigs []*UpgradeConfig `json:"upgrade_configs,omitempty"`

	// StreamIdleTimeout specifies the idle timeout for streams.
	StreamIdleTimeout string `json:"stream_idle_timeout,omitempty"`

	// RequestTimeout specifies the total request timeout.
	RequestTimeout string `json:"request_timeout,omitempty"`

	// AccessLog configures access logging for this HCM.
	// +kubebuilder:pruning:PreserveUnknownFields
	AccessLog []*AccessLogConfig `json:"access_log,omitempty"`

	// Tracing configures distributed tracing.
	// +kubebuilder:pruning:PreserveUnknownFields
	Tracing *runtime.RawExtension `json:"tracing,omitempty"`

	// CommonHttpProtocolOptions configures HTTP protocol options.
	// +kubebuilder:pruning:PreserveUnknownFields
	CommonHttpProtocolOptions *runtime.RawExtension `json:"common_http_protocol_options,omitempty"`

	// ServerHeaderTransformation specifies how to handle Server header.
	ServerHeaderTransformation string `json:"server_header_transformation,omitempty"`

	// SchemeHeaderTransformation specifies how to handle scheme header.
	// +kubebuilder:pruning:PreserveUnknownFields
	SchemeHeaderTransformation *runtime.RawExtension `json:"scheme_header_transformation,omitempty"`

	// PathWithEscapedSlashesAction specifies how to handle escaped slashes.
	PathWithEscapedSlashesAction string `json:"path_with_escaped_slashes_action,omitempty"`

	// NormalizePath enables path normalization.
	NormalizePath *bool `json:"normalize_path,omitempty"`

	// MergeSlashes enables merging adjacent slashes.
	MergeSlashes bool `json:"merge_slashes,omitempty"`

	// StripPortMode specifies port stripping behavior.
	// +kubebuilder:pruning:PreserveUnknownFields
	StripPortMode *runtime.RawExtension `json:"strip_port_mode,omitempty"`

	// LocalReplyConfig customizes local replies.
	// +kubebuilder:pruning:PreserveUnknownFields
	LocalReplyConfig *runtime.RawExtension `json:"local_reply_config,omitempty"`
}

// HTTPFilter configures an HTTP filter in the filter chain.
type HTTPFilter struct {
	// Name is the filter name (e.g., envoy.filters.http.router).
	Name string `json:"name,omitempty"`

	// TypedConfig is the filter configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	TypedConfig *runtime.RawExtension `json:"typed_config,omitempty"`
}

// UpgradeConfig configures protocol upgrades.
type UpgradeConfig struct {
	// UpgradeType is the upgrade type (e.g., "websocket").
	UpgradeType string `json:"upgrade_type,omitempty"`

	// Enabled specifies if this upgrade is enabled.
	Enabled *bool `json:"enabled,omitempty"`
}

// AccessLogConfig configures an access log.
type AccessLogConfig struct {
	// Name is the access log name.
	Name string `json:"name,omitempty"`

	// Filter is the access log filter.
	// +kubebuilder:pruning:PreserveUnknownFields
	Filter *runtime.RawExtension `json:"filter,omitempty"`

	// TypedConfig is the access log configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	TypedConfig *runtime.RawExtension `json:"typed_config,omitempty"`
}

// RDSConfig configures route discovery via RDS API.
type RDSConfig struct {
	// RouteConfigName is the name of the route configuration to fetch.
	RouteConfigName string `json:"route_config_name,omitempty"`

	// ConfigSource specifies where to fetch the route configuration.
	ConfigSource *ConfigSource `json:"config_source,omitempty"`
}

// ConfigSource specifies a configuration source.
type ConfigSource struct {
	// ADS uses Aggregated Discovery Service.
	ADS *AggregatedConfigSource `json:"ads,omitempty"`

	// ResourceApiVersion specifies the API version (e.g., "V3").
	ResourceApiVersion string `json:"resource_api_version,omitempty"`
}

// AggregatedConfigSource configures ADS.
type AggregatedConfigSource struct{}

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
	if in.RDS != nil {
		in, out := &in.RDS, &out.RDS
		*out = new(RDSConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.HttpFilters != nil {
		in, out := &in.HttpFilters, &out.HttpFilters
		*out = make([]*HTTPFilter, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				(*out)[i] = new(HTTPFilter)
				(*in)[i].DeepCopyInto((*out)[i])
			}
		}
	}
	if in.GenerateRequestId != nil {
		in, out := &in.GenerateRequestId, &out.GenerateRequestId
		*out = new(bool)
		**out = **in
	}
	if in.UseRemoteAddress != nil {
		in, out := &in.UseRemoteAddress, &out.UseRemoteAddress
		*out = new(bool)
		**out = **in
	}
	if in.UpgradeConfigs != nil {
		in, out := &in.UpgradeConfigs, &out.UpgradeConfigs
		*out = make([]*UpgradeConfig, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				(*out)[i] = new(UpgradeConfig)
				(*in)[i].DeepCopyInto((*out)[i])
			}
		}
	}
	if in.AccessLog != nil {
		in, out := &in.AccessLog, &out.AccessLog
		*out = make([]*AccessLogConfig, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				(*out)[i] = new(AccessLogConfig)
				(*in)[i].DeepCopyInto((*out)[i])
			}
		}
	}
	if in.Tracing != nil {
		in, out := &in.Tracing, &out.Tracing
		*out = new(runtime.RawExtension)
		**out = **in
	}
	if in.CommonHttpProtocolOptions != nil {
		in, out := &in.CommonHttpProtocolOptions, &out.CommonHttpProtocolOptions
		*out = new(runtime.RawExtension)
		**out = **in
	}
	if in.SchemeHeaderTransformation != nil {
		in, out := &in.SchemeHeaderTransformation, &out.SchemeHeaderTransformation
		*out = new(runtime.RawExtension)
		**out = **in
	}
	if in.NormalizePath != nil {
		in, out := &in.NormalizePath, &out.NormalizePath
		*out = new(bool)
		**out = **in
	}
	if in.StripPortMode != nil {
		in, out := &in.StripPortMode, &out.StripPortMode
		*out = new(runtime.RawExtension)
		**out = **in
	}
	if in.LocalReplyConfig != nil {
		in, out := &in.LocalReplyConfig, &out.LocalReplyConfig
		*out = new(runtime.RawExtension)
		**out = **in
	}
}

// DeepCopyInto for HTTPFilter
func (in *HTTPFilter) DeepCopyInto(out *HTTPFilter) {
	*out = *in
	if in.TypedConfig != nil {
		in, out := &in.TypedConfig, &out.TypedConfig
		*out = new(runtime.RawExtension)
		**out = **in
	}
}

// DeepCopyInto for UpgradeConfig
func (in *UpgradeConfig) DeepCopyInto(out *UpgradeConfig) {
	*out = *in
	if in.Enabled != nil {
		in, out := &in.Enabled, &out.Enabled
		*out = new(bool)
		**out = **in
	}
}

// DeepCopyInto for AccessLogConfig
func (in *AccessLogConfig) DeepCopyInto(out *AccessLogConfig) {
	*out = *in
	if in.Filter != nil {
		in, out := &in.Filter, &out.Filter
		*out = new(runtime.RawExtension)
		**out = **in
	}
	if in.TypedConfig != nil {
		in, out := &in.TypedConfig, &out.TypedConfig
		*out = new(runtime.RawExtension)
		**out = **in
	}
}

// DeepCopyInto for RDSConfig
func (in *RDSConfig) DeepCopyInto(out *RDSConfig) {
	*out = *in
	if in.ConfigSource != nil {
		in, out := &in.ConfigSource, &out.ConfigSource
		*out = new(ConfigSource)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopyInto for ConfigSource
func (in *ConfigSource) DeepCopyInto(out *ConfigSource) {
	*out = *in
	if in.ADS != nil {
		in, out := &in.ADS, &out.ADS
		*out = new(AggregatedConfigSource)
		**out = **in
	}
}
