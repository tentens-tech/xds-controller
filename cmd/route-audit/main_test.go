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

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runAudit(t *testing.T, manifests string) ([]string, int) {
	t.Helper()
	report, problems, err := audit(strings.NewReader(manifests), "global", "global")
	require.NoError(t, err)
	return report, problems
}

const clusterShape = `
apiVersion: v1
kind: List
items:
- apiVersion: envoyxds.io/v1alpha1
  kind: Route
  metadata: {name: edge-catch-all, creationTimestamp: "2025-01-01T00:00:00Z", annotations: {nodes: "01,02", clusters: edge}}
  spec: {listener_refs: [https, quic]}
- apiVersion: envoyxds.io/v1alpha1
  kind: Route
  metadata: {name: edge-shop, creationTimestamp: "2025-01-02T00:00:00Z", annotations: {nodes: "01,02", clusters: edge}}
  spec: {listener_refs: [https], filter_chain_match: {server_names: [shop.example.com]}}
- apiVersion: envoyxds.io/v1alpha1
  kind: Route
  metadata: {name: apps-shop, creationTimestamp: "2025-02-01T00:00:00Z", annotations: {nodes: "01,02", clusters: apps}}
  spec: {listener_refs: [https, quic], filter_chain_match: {server_names: [shop.example.com]}}
- apiVersion: envoyxds.io/v1alpha1
  kind: Route
  metadata: {name: cdn-chain, creationTimestamp: "2025-01-03T00:00:00Z", annotations: {nodes: "01,02", clusters: apps}}
  spec:
    listener_refs: [https]
    filter_chain_match:
      source_prefix_ranges: [{address_prefix: 35.191.0.0, prefix_len: 16}, {address_prefix: 130.211.0.0, prefix_len: 22}]
      application_protocols: [h2, http/1.1]
- apiVersion: envoyxds.io/v1alpha1
  kind: Route
  metadata: {name: lb-health-probe, creationTimestamp: "2025-03-01T00:00:00Z", annotations: {nodes: "01,02", clusters: apps}}
  spec:
    listener_refs: [https]
    filter_chain_match:
      source_prefix_ranges: [{address_prefix: 35.191.0.0, prefix_len: 16}, {address_prefix: 130.211.0.0, prefix_len: 22}]
- apiVersion: envoyxds.io/v1alpha1
  kind: Route
  metadata: {name: lb-health-probe-copy, creationTimestamp: "2025-03-02T00:00:00Z", annotations: {nodes: "01", clusters: apps}}
  spec:
    listener_refs: [https]
    filter_chain_match:
      source_prefix_ranges: [{address_prefix: 35.191.0.0, prefix_len: 16}]
- apiVersion: envoyxds.io/v1alpha1
  kind: Route
  metadata: {name: broken, creationTimestamp: "2025-03-03T00:00:00Z", annotations: {nodes: "01", clusters: apps}}
  spec:
    listener_refs: [https]
    filter_chain_match:
      source_prefix_ranges: [{address_prefix: not-an-ip, prefix_len: 8}]
- apiVersion: route.openshift.io/v1
  kind: Route
  metadata: {name: not-ours}
  spec: {host: example.com}
`

func TestAudit(t *testing.T) {
	report, problems := runAudit(t, clusterShape)
	assert.Equal(t, 2, problems)
	assert.Equal(t, []string{
		"CONFLICT lb-health-probe evicts lb-health-probe-copy (duplicate) on apps/01",
		`INVALID  broken: source_prefix_ranges: malformed IP address "not-an-ip"`,
		"7 routes, 2 problems",
	}, report)
}

func TestAudit_UnappliedManifestIsNewest(t *testing.T) {
	report, _ := runAudit(t, `
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata: {name: a-new-probe}
spec: {listener_refs: [https], filter_chain_match: {source_prefix_ranges: [{address_prefix: 10.0.0.0, prefix_len: 8}]}}
---
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata: {name: z-live-probe, creationTimestamp: "2025-01-01T00:00:00Z"}
spec: {listener_refs: [https], filter_chain_match: {source_prefix_ranges: [{address_prefix: 10.0.0.0, prefix_len: 8}]}}
`)
	assert.Equal(t, "CONFLICT z-live-probe evicts a-new-probe (duplicate) on global/global", report[0])
}

func TestAudit_EvictedRoutesDoNotEvictOthers(t *testing.T) {
	report, problems := runAudit(t, `
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata: {name: a, creationTimestamp: "2025-01-01T00:00:00Z"}
spec: {listener_refs: [https], filter_chain_match: {source_prefix_ranges: [{address_prefix: 10.0.0.0, prefix_len: 8}]}}
---
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata: {name: b, creationTimestamp: "2025-01-02T00:00:00Z"}
spec: {listener_refs: [https], filter_chain_match: {source_prefix_ranges: [{address_prefix: 10.0.0.0, prefix_len: 8}, {address_prefix: 20.0.0.0, prefix_len: 8}]}}
---
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata: {name: c, creationTimestamp: "2025-01-03T00:00:00Z"}
spec: {listener_refs: [https], filter_chain_match: {source_prefix_ranges: [{address_prefix: 20.0.0.0, prefix_len: 8}]}}
`)
	assert.Equal(t, 1, problems, report)
	assert.Equal(t, "CONFLICT a evicts b (duplicate) on global/global", report[0])
}

func TestAudit_ListenersFromInput(t *testing.T) {
	report, problems := runAudit(t, `
apiVersion: envoyxds.io/v1alpha1
kind: Listener
metadata: {name: tcp, annotations: {clusters: apps, nodes: "01"}}
spec:
  filter_chains:
    - filters: []
---
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata: {name: catch-all, creationTimestamp: "2025-01-01T00:00:00Z", annotations: {clusters: apps, nodes: "01,02"}}
spec: {listener_refs: [tcp]}
---
apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata: {name: elsewhere, creationTimestamp: "2025-01-02T00:00:00Z", annotations: {clusters: apps, nodes: "02"}}
spec: {listener_refs: [tcp]}
`)
	assert.Equal(t, 1, problems, report)
	assert.Equal(t, "CONFLICT Listener/tcp spec.filter_chains evicts catch-all (duplicate) on apps/01", report[0])
}

func TestAuditMultiDocument(t *testing.T) {
	data, err := os.ReadFile("../../examples/grpc/resources.yaml")
	require.NoError(t, err)
	report, problems, err := audit(bytes.NewReader(data), "global", "global")
	require.NoError(t, err)
	assert.Zero(t, problems, report)
	assert.Equal(t, []string{"2 routes, 0 problems"}, report)
}

func TestReadInputsKeepsFilesApart(t *testing.T) {
	dir := t.TempDir()
	live := dir + "/live.yaml"
	fresh := dir + "/new.yaml"
	require.NoError(t, os.WriteFile(live, []byte(`apiVersion: v1
kind: List
items:
- apiVersion: envoyxds.io/v1alpha1
  kind: Route
  metadata: {name: live-probe, creationTimestamp: "2025-01-01T00:00:00Z"}
  spec: {listener_refs: [https], filter_chain_match: {source_prefix_ranges: [{address_prefix: 10.0.0.0, prefix_len: 8}]}}
`), 0o600))
	require.NoError(t, os.WriteFile(fresh, []byte(`apiVersion: envoyxds.io/v1alpha1
kind: Route
metadata: {name: new-probe}
spec: {listener_refs: [https], filter_chain_match: {source_prefix_ranges: [{address_prefix: 10.0.0.0, prefix_len: 8}]}}
`), 0o600))

	in, err := readInputs([]string{live, fresh})
	require.NoError(t, err)
	report, problems, err := audit(bytes.NewReader(in), "global", "global")
	require.NoError(t, err)
	assert.Equal(t, 1, problems)
	assert.Equal(t, []string{
		"CONFLICT live-probe evicts new-probe (duplicate) on global/global",
		"2 routes, 1 problems",
	}, report)
}
