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

// Package fcm decides whether two filter chain matchers can share one Envoy listener.
package fcm

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
)

// Verdict is the result of comparing two matchers.
type Verdict uint8

const (
	// None means Envoy tells the two chains apart.
	None Verdict = iota
	// Shadow means server names overlap only through a wildcard or a trailing dot.
	// Envoy accepts it, but the controller treats it as a conflict when virtual hosts overlap.
	Shadow
	// Duplicate means Envoy rejects a listener holding both chains, or picks one of
	// them by declaration order (an explicit 0.0.0.0/0 or ::/0 next to an unset range).
	Duplicate
)

func (v Verdict) String() string {
	switch v {
	case Shadow:
		return "shadow"
	case Duplicate:
		return "duplicate"
	default:
		return "none"
	}
}

// Matcher is a filter chain match normalized to the keys Envoy files chains under.
// An unset criterion holds the key Envoy uses for "any".
type Matcher struct {
	destinationPort   uint32
	prefixRanges      []netip.Prefix
	serverNames       []string
	transportProtocol string
	alpn              []string
	directSource      []netip.Prefix
	sourceType        listenerv3.FilterChainMatch_ConnectionSourceType
	source            []netip.Prefix
	sourcePorts       []uint32
}

var (
	anyNames     = []string{""}
	anyPorts     = []uint32{0}
	anyAddresses = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
)

// Compile validates m the way the listener build parses it and normalizes it. A nil m is the catch-all matcher.
func Compile(m *lds.FilterChainMatch) (*Matcher, error) {
	if m == nil {
		return CompileProto(nil)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	p := &listenerv3.FilterChainMatch{}
	if err := protojson.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return CompileProto(p)
}

// CompileProto validates m against the proto rules and the checks Envoy runs when
// adding a listener, then normalizes it. A nil m is the catch-all matcher.
func CompileProto(m *listenerv3.FilterChainMatch) (*Matcher, error) {
	if m == nil {
		m = &listenerv3.FilterChainMatch{}
	}
	if err := m.ValidateAll(); err != nil {
		return nil, err
	}
	if m.GetAddressSuffix() != "" || m.GetSuffixLen() != nil {
		return nil, fmt.Errorf("address_suffix and suffix_len are not implemented by Envoy")
	}

	c := &Matcher{
		destinationPort:   m.GetDestinationPort().GetValue(),
		transportProtocol: m.GetTransportProtocol(),
		sourceType:        m.GetSourceType(),
	}
	var err error
	if c.serverNames, err = compileServerNames(m.GetServerNames()); err != nil {
		return nil, err
	}
	if c.alpn, err = unique("application_protocols", m.GetApplicationProtocols(), anyNames); err != nil {
		return nil, err
	}
	if c.sourcePorts, err = unique("source_ports", m.GetSourcePorts(), anyPorts); err != nil {
		return nil, err
	}
	if c.prefixRanges, err = compileCIDRs("prefix_ranges", m.GetPrefixRanges()); err != nil {
		return nil, err
	}
	if c.directSource, err = compileCIDRs("direct_source_prefix_ranges", m.GetDirectSourcePrefixRanges()); err != nil {
		return nil, err
	}
	if c.source, err = compileCIDRs("source_prefix_ranges", m.GetSourcePrefixRanges()); err != nil {
		return nil, err
	}
	return c, nil
}

// Envoy rejects a chain that lists the same key twice, and files an empty list under the "any" key.
func unique[T comparable](field string, values, empty []T) ([]T, error) {
	if len(values) == 0 {
		return empty, nil
	}
	for i, v := range values {
		if slices.Contains(values[:i], v) {
			return nil, fmt.Errorf("%s: %v listed twice", field, v)
		}
	}
	return values, nil
}

// Envoy lowercases server names (ASCII only) and files "*.example.com" under ".example.com".
func compileServerNames(names []string) ([]string, error) {
	keys := make([]string, len(names))
	for i, n := range names {
		if strings.Contains(n, "*") && !strings.HasPrefix(n, "*.") || strings.Count(n, "*") > 1 {
			return nil, fmt.Errorf("server_names: partial wildcards are not supported: %q", n)
		}
		keys[i] = strings.TrimPrefix(asciiLower(n), "*")
	}
	return unique("server_names", keys, anyNames)
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// Envoy clamps prefix_len to the address length and masks the address to the prefix.
func compileCIDRs(field string, ranges []*corev3.CidrRange) ([]netip.Prefix, error) {
	if len(ranges) == 0 {
		return anyAddresses, nil
	}
	out := make([]netip.Prefix, 0, len(ranges))
	for _, r := range ranges {
		addr, err := netip.ParseAddr(r.GetAddressPrefix())
		if err != nil {
			return nil, fmt.Errorf("%s: malformed IP address %q", field, r.GetAddressPrefix())
		}
		p, err := addr.Prefix(min(int(r.GetPrefixLen().GetValue()), addr.BitLen()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		out = append(out, p)
	}
	return unique(field, out, anyAddresses)
}

// Compare reports how Envoy treats a listener holding chains with both matchers.
// Envoy walks the criteria in a fixed order and keys each on exact values, so two
// chains collide only when every criterion shares a key.
func (m *Matcher) Compare(o *Matcher) Verdict {
	if m.destinationPort != o.destinationPort ||
		m.transportProtocol != o.transportProtocol ||
		m.sourceType != o.sourceType ||
		!shareKey(m.prefixRanges, o.prefixRanges) ||
		!shareKey(m.alpn, o.alpn) ||
		!shareKey(m.directSource, o.directSource) ||
		!shareKey(m.source, o.source) ||
		!shareKey(m.sourcePorts, o.sourcePorts) {
		return None
	}
	return compareServerNames(m.serverNames, o.serverNames)
}

// Compare compiles both matchers and compares them.
func Compare(a, b *lds.FilterChainMatch) (Verdict, error) {
	ca, err := Compile(a)
	if err != nil {
		return None, err
	}
	cb, err := Compile(b)
	if err != nil {
		return None, err
	}
	return ca.Compare(cb), nil
}

func shareKey[T comparable](a, b []T) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func compareServerNames(a, b []string) Verdict {
	v := None
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return Duplicate
			}
			if x != "" && y != "" && (covers(x, y) || covers(y, x) || sameButTrailingDot(x, y)) {
				v = Shadow
			}
		}
	}
	return v
}

func sameButTrailingDot(x, y string) bool {
	switch len(x) - len(y) {
	case 1:
		return x[len(x)-1] == '.' && x[:len(y)] == y
	case -1:
		return y[len(y)-1] == '.' && y[:len(x)] == x
	}
	return false
}

// MatchDomainName reports whether the server_names entry pattern covers domain.
// A wildcard covers exactly one extra label, so "*.example.com" does not cover "example.com".
func MatchDomainName(pattern, domain string) bool {
	pattern = strings.TrimPrefix(asciiLower(strings.TrimSuffix(pattern, ".")), "*")
	domain = strings.TrimPrefix(asciiLower(strings.TrimSuffix(domain, ".")), "*")
	return pattern == domain || covers(pattern, domain)
}

// covers takes normalized keys, where a wildcard starts with ".".
func covers(pattern, domain string) bool {
	if !strings.HasPrefix(pattern, ".") {
		return false
	}
	label, rest, ok := strings.Cut(domain, ".")
	return ok && label != "" && "."+rest == pattern
}
