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

package fcm

import (
	"encoding/json"
	"slices"
	"testing"

	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
)

func u32(v uint32) *uint32 { return &v }

func cidr(addr string, bits uint32) *lds.CidrRange {
	return &lds.CidrRange{AddressPrefix: addr, PrefixLen: u32(bits)}
}

var lbRanges = []*lds.CidrRange{cidr("35.191.0.0", 16), cidr("130.211.0.0", 22)}

type matchCase struct {
	name string
	a, b *lds.FilterChainMatch
	want Verdict
}

// Every case here is also checked against a real Envoy by TestEnvoyParity.
var matchCases = []matchCase{
	{"health probe next to cdn chain", &lds.FilterChainMatch{SourcePrefixRanges: lbRanges, ApplicationProtocols: []string{"h2", "http/1.1"}}, &lds.FilterChainMatch{SourcePrefixRanges: lbRanges}, None},
	{"two nil matchers", nil, nil, Duplicate},
	{"nil and empty matcher", nil, &lds.FilterChainMatch{}, Duplicate},
	{"identical source ranges", &lds.FilterChainMatch{SourcePrefixRanges: lbRanges}, &lds.FilterChainMatch{SourcePrefixRanges: lbRanges}, Duplicate},
	{"one shared source range", &lds.FilterChainMatch{SourcePrefixRanges: lbRanges}, &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("35.191.0.0", 16)}}, Duplicate},
	{"nested source ranges", &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("35.191.0.0", 16)}}, &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("35.191.0.0", 17)}}, None},
	{"unmasked source range", &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("35.191.7.7", 16)}}, &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("35.191.0.0", 16)}}, Duplicate},
	{"unset prefix_len is /0", &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{{AddressPrefix: "1.2.3.4"}}}, &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{{AddressPrefix: "5.6.7.8"}}}, Duplicate},
	{"prefix_len clamps to address length", &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("1.2.3.4", 33)}}, &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("1.2.3.4", 32)}}, Duplicate},
	{"v4-mapped v6 differs from v4", &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("::ffff:1.2.3.4", 128)}}, &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("1.2.3.4", 32)}}, None},
	{"direct source ranges", &lds.FilterChainMatch{DirectSourcePrefixRanges: []*lds.CidrRange{cidr("10.0.0.0", 8)}}, &lds.FilterChainMatch{DirectSourcePrefixRanges: []*lds.CidrRange{cidr("10.0.0.0", 8)}}, Duplicate},
	{"destination prefix ranges", &lds.FilterChainMatch{PrefixRanges: []*lds.CidrRange{cidr("10.0.0.0", 8)}}, &lds.FilterChainMatch{PrefixRanges: []*lds.CidrRange{cidr("10.0.0.0", 8)}}, Duplicate},
	{"intersecting alpn", &lds.FilterChainMatch{ApplicationProtocols: []string{"h2", "http/1.1"}}, &lds.FilterChainMatch{ApplicationProtocols: []string{"h2"}}, Duplicate},
	{"disjoint alpn", &lds.FilterChainMatch{ApplicationProtocols: []string{"h2"}}, &lds.FilterChainMatch{ApplicationProtocols: []string{"http/1.1"}}, None},
	{"empty alpn list is unset", &lds.FilterChainMatch{ApplicationProtocols: []string{}}, nil, Duplicate},
	{"wildcard covers exact name", &lds.FilterChainMatch{ServerNames: []string{"*.example.com"}}, &lds.FilterChainMatch{ServerNames: []string{"api.example.com"}}, Shadow},
	{"same wildcard", &lds.FilterChainMatch{ServerNames: []string{"*.example.com"}}, &lds.FilterChainMatch{ServerNames: []string{"*.example.com"}}, Duplicate},
	{"server names ignore case", &lds.FilterChainMatch{ServerNames: []string{"Example.com"}}, &lds.FilterChainMatch{ServerNames: []string{"example.com"}}, Duplicate},
	{"different server names", &lds.FilterChainMatch{ServerNames: []string{"a.example.com"}}, &lds.FilterChainMatch{ServerNames: []string{"b.example.com"}}, None},
	{"server names set and unset", &lds.FilterChainMatch{ServerNames: []string{"example.com"}}, nil, None},
	{"same server name, different alpn", &lds.FilterChainMatch{ServerNames: []string{"example.com"}, ApplicationProtocols: []string{"h2"}}, &lds.FilterChainMatch{ServerNames: []string{"example.com"}}, None},
	{"destination port set and unset", &lds.FilterChainMatch{DestinationPort: u32(443)}, nil, None},
	{"same destination port", &lds.FilterChainMatch{DestinationPort: u32(443)}, &lds.FilterChainMatch{DestinationPort: u32(443)}, Duplicate},
	{"different destination ports", &lds.FilterChainMatch{DestinationPort: u32(443)}, &lds.FilterChainMatch{DestinationPort: u32(8443)}, None},
	{"transport protocol set and unset", &lds.FilterChainMatch{TransportProtocol: "tls"}, nil, None},
	{"source type ANY is unset", &lds.FilterChainMatch{SourceType: lds.FilterChainMatch_ConnectionSourceType_ANY}, nil, Duplicate},
	{"source type EXTERNAL", &lds.FilterChainMatch{SourceType: lds.FilterChainMatch_ConnectionSourceType_EXTERNAL}, nil, None},
	{"intersecting source ports", &lds.FilterChainMatch{SourcePorts: []uint32{1, 2}}, &lds.FilterChainMatch{SourcePorts: []uint32{2, 3}}, Duplicate},
	{"disjoint source ports", &lds.FilterChainMatch{SourcePorts: []uint32{1}}, &lds.FilterChainMatch{SourcePorts: []uint32{2}}, None},
	{"explicit empty server name is unset", &lds.FilterChainMatch{ServerNames: []string{""}}, nil, Duplicate},
	{"explicit empty alpn is unset", &lds.FilterChainMatch{ApplicationProtocols: []string{""}}, nil, Duplicate},
	{"leading dot equals wildcard", &lds.FilterChainMatch{ServerNames: []string{".example.com"}}, &lds.FilterChainMatch{ServerNames: []string{"*.example.com"}}, Duplicate},
	{"trailing dot is another name", &lds.FilterChainMatch{ServerNames: []string{"example.com."}}, &lds.FilterChainMatch{ServerNames: []string{"example.com"}}, Shadow},
	{"non-ASCII case is not folded", &lds.FilterChainMatch{ServerNames: []string{"\u212a.com"}}, &lds.FilterChainMatch{ServerNames: []string{"k.com"}}, None},
	{"wildcard two labels up", &lds.FilterChainMatch{ServerNames: []string{"*.example.com"}}, &lds.FilterChainMatch{ServerNames: []string{"a.b.example.com"}}, None},
	{"raw_buffer is not unset", &lds.FilterChainMatch{TransportProtocol: "raw_buffer"}, nil, None},
}

// Envoy accepts each pair but picks a chain by declaration order, which differs
// between Envoy versions, so the controller treats them as a conflict.
var ambiguousCases = []matchCase{
	{"explicit 0.0.0.0/0 next to unset source range", &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("0.0.0.0", 0)}}, nil, Duplicate},
	{"explicit ::/0 next to unset source range", &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("::", 0)}}, nil, Duplicate},
	{"explicit 0.0.0.0/0 next to unset destination range", &lds.FilterChainMatch{PrefixRanges: []*lds.CidrRange{cidr("0.0.0.0", 0)}}, nil, Duplicate},
}

// Envoy rejects each of these on its own, for the given reason.
var invalidCases = []struct {
	name   string
	m      *lds.FilterChainMatch
	reason string
}{
	{"malformed address", &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("not-an-ip", 8)}}, "malformed IP address"},
	{"destination port zero", &lds.FilterChainMatch{DestinationPort: u32(0)}, "validation failed"},
	{"destination port above 65535", &lds.FilterChainMatch{DestinationPort: u32(70000)}, "validation failed"},
	{"prefix_len above 128", &lds.FilterChainMatch{SourcePrefixRanges: []*lds.CidrRange{cidr("1.2.3.4", 200)}}, "validation failed"},
	{"source port zero", &lds.FilterChainMatch{SourcePorts: []uint32{0}}, "validation failed"},
	{"source port above 65535", &lds.FilterChainMatch{SourcePorts: []uint32{70000}}, "validation failed"},
	{"bare wildcard server name", &lds.FilterChainMatch{ServerNames: []string{"*"}}, "partial wildcards"},
	{"wildcard without dot", &lds.FilterChainMatch{ServerNames: []string{"*example.com"}}, "partial wildcards"},
	{"address_suffix", &lds.FilterChainMatch{AddressSuffix: "1.2.3.4"}, "unimplemented fields"},
	{"suffix_len", &lds.FilterChainMatch{SuffixLen: u32(8)}, "unimplemented fields"},
	{"server name twice ignoring case", &lds.FilterChainMatch{ServerNames: []string{"a.com", "A.com"}}, "matching rules"},
	{"alpn twice", &lds.FilterChainMatch{ApplicationProtocols: []string{"h2", "h2"}}, "matching rules"},
	{"ranges equal after masking", &lds.FilterChainMatch{PrefixRanges: []*lds.CidrRange{cidr("10.0.0.0", 8), cidr("10.9.0.0", 8)}}, "matching rules"},
	{"source port twice", &lds.FilterChainMatch{SourcePorts: []uint32{1, 1}}, "matching rules"},
}

func toProto(t testing.TB, m *lds.FilterChainMatch) *listenerv3.FilterChainMatch {
	t.Helper()
	if m == nil {
		return nil
	}
	data, err := json.Marshal(m)
	require.NoError(t, err)
	p := &listenerv3.FilterChainMatch{}
	require.NoError(t, protojson.Unmarshal(data, p))
	return p
}

func TestCompare(t *testing.T) {
	for _, tc := range slices.Concat(matchCases, ambiguousCases) {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compare(tc.a, tc.b)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got, "a vs b")

			got, err = Compare(tc.b, tc.a)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got, "b vs a")
		})
	}
}

func TestCompileProtoMatchesCompile(t *testing.T) {
	for _, tc := range slices.Concat(matchCases, ambiguousCases) {
		t.Run(tc.name, func(t *testing.T) {
			a, err := CompileProto(toProto(t, tc.a))
			require.NoError(t, err)
			b, err := Compile(tc.b)
			require.NoError(t, err)
			assert.Equal(t, tc.want, a.Compare(b))
		})
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileProto(toProto(t, tc.m))
			assert.Error(t, err)
		})
	}
}

func TestCompileInvalid(t *testing.T) {
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.m)
			assert.Error(t, err)
		})
	}
	// Envoy accepts lowercase enum names, but the listener build's protojson does not.
	_, err := Compile(&lds.FilterChainMatch{SourceType: "external"})
	assert.Error(t, err)
}

func TestMatchDomainName(t *testing.T) {
	tests := []struct {
		pattern, domain string
		want            bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "other.com", false},
		{"*.example.com", "www.example.com", true},
		{"*.example.com", "api.example.com", true},
		{"*.example.com", "www.other.com", false},
		{"*.example.com", "example.com", false},
		{"*.example.com", "a.b.example.com", false},
		{"example.com.", "example.com", true},
		{"example.com", "example.com.", true},
		{"*.Example.COM", "api.example.com", true},
		{".example.com", "api.example.com", true},
		{"*.com", "com", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.domain, func(t *testing.T) {
			assert.Equal(t, tt.want, MatchDomainName(tt.pattern, tt.domain))
		})
	}
}

func TestVerdictString(t *testing.T) {
	assert.Equal(t, "none", None.String())
	assert.Equal(t, "shadow", Shadow.String())
	assert.Equal(t, "duplicate", Duplicate.String())
}

func BenchmarkCompile(b *testing.B) {
	m := &lds.FilterChainMatch{
		SourcePrefixRanges:   lbRanges,
		ApplicationProtocols: []string{"h2", "http/1.1"},
		ServerNames:          []string{"api.example.com", "*.shop.example.com"},
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Compile(m); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompare(b *testing.B) {
	compile := func(m *lds.FilterChainMatch) *Matcher {
		c, err := Compile(m)
		require.NoError(b, err)
		return c
	}
	x := compile(&lds.FilterChainMatch{SourcePrefixRanges: lbRanges, ApplicationProtocols: []string{"h2", "http/1.1"}})
	y := compile(&lds.FilterChainMatch{SourcePrefixRanges: lbRanges})
	names1 := compile(&lds.FilterChainMatch{ServerNames: []string{"a.example.com", "b.example.com", "*.c.example.com"}})
	names2 := compile(&lds.FilterChainMatch{ServerNames: []string{"x.example.com", "y.c.example.com"}})
	b.Run("criteria differ", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = x.Compare(y)
		}
	})
	b.Run("server names", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = names1.Compare(names2)
		}
	})
}
