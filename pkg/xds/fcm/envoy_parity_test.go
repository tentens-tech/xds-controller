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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tentens-tech/xds-controller/pkg/xds/types/lds"
)

// TestEnvoyParity checks every verdict against `envoy --mode validate`.
// Set ENVOY_PARITY_IMAGE to one or more comma-separated images
// (e.g. envoyproxy/envoy:v1.36-latest) to run it; needs docker.
func TestEnvoyParity(t *testing.T) {
	images := os.Getenv("ENVOY_PARITY_IMAGE")
	if images == "" {
		t.Skip("ENVOY_PARITY_IMAGE not set")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("ENVOY_PARITY_IMAGE is set but docker is not available")
	}

	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o755)) //nolint:gosec // the envoy container user must read the configs

	type expectation struct {
		name, want string
	}
	var want []expectation
	write := func(name, verdict string, chains ...*lds.FilterChainMatch) {
		file := fmt.Sprintf("%03d.json", len(want))
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), envoyConfig(t, chains...), 0o644)) //nolint:gosec // read by the container user
		want = append(want, expectation{name: name, want: verdict})
	}
	for _, tc := range matchCases {
		verdict := "ok"
		if tc.want == Duplicate {
			verdict = "rejected: matching rules"
		}
		write(tc.name, verdict, tc.a, tc.b)
	}
	for _, tc := range ambiguousCases {
		write(tc.name, "ok", tc.a, tc.b)
	}
	for _, tc := range invalidCases {
		write(tc.name, "rejected: "+tc.reason, tc.m)
	}

	script := `for f in /c/*.json; do
  if envoy --mode validate -c "$f" --log-level critical >/tmp/out 2>&1; then echo "$(basename $f) ok"
  else echo "$(basename $f) rejected: $(grep -o -m1 'matching rules\|malformed IP address\|validation failed\|partial wildcards\|unimplemented fields' /tmp/out)"; fi
done`
	for image := range strings.SplitSeq(images, ",") {
		t.Run(image, func(t *testing.T) {
			out, err := exec.CommandContext(t.Context(), "docker", "run", "--rm", "-v", dir+":/c:ro", "--entrypoint", "/bin/sh", image, "-c", script).CombinedOutput() //nolint:gosec // test-only command
			require.NoError(t, err, string(out))

			got := make(map[string]string)
			sc := bufio.NewScanner(bytes.NewReader(out))
			for sc.Scan() {
				if file, verdict, ok := strings.Cut(sc.Text(), " "); ok && strings.HasSuffix(file, ".json") {
					got[file] = verdict
				}
			}
			for i, w := range want {
				file := fmt.Sprintf("%03d.json", i)
				verdict, ok := got[file]
				require.True(t, ok, "no envoy result for %q", w.name)
				require.Equal(t, w.want, verdict, w.name)
			}
		})
	}
}

func envoyConfig(t testing.TB, chains ...*lds.FilterChainMatch) []byte {
	t.Helper()
	hcm := map[string]any{
		"@type":       "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
		"stat_prefix": "s",
		"route_config": map[string]any{"virtual_hosts": []any{map[string]any{
			"name": "v", "domains": []string{"*"},
			"routes": []any{map[string]any{"match": map[string]any{"prefix": "/"}, "direct_response": map[string]any{"status": 200}}},
		}}},
		"http_filters": []any{map[string]any{
			"name":         "envoy.filters.http.router",
			"typed_config": map[string]any{"@type": "type.googleapis.com/envoy.extensions.filters.http.router.v3.Router"},
		}},
	}
	filterChains := make([]any, 0, len(chains))
	for _, m := range chains {
		fc := map[string]any{"filters": []any{map[string]any{"name": "envoy.filters.network.http_connection_manager", "typed_config": hcm}}}
		if m != nil {
			fc["filter_chain_match"] = m
		}
		filterChains = append(filterChains, fc)
	}
	cfg := map[string]any{"static_resources": map[string]any{"listeners": []any{map[string]any{
		"name":    "l",
		"address": map[string]any{"socket_address": map[string]any{"address": "0.0.0.0", "port_value": 10443}},
		"listener_filters": []any{map[string]any{
			"name":         "envoy.filters.listener.tls_inspector",
			"typed_config": map[string]any{"@type": "type.googleapis.com/envoy.extensions.filters.listener.tls_inspector.v3.TlsInspector"},
		}},
		"filter_chains": filterChains,
	}}}}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	return data
}
