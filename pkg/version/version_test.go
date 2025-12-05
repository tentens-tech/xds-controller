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

package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()

	if info.Version == "" {
		t.Error("expected Version to be set")
	}
	if info.GitCommit == "" {
		t.Error("expected GitCommit to be set")
	}
	if info.BuildDate == "" {
		t.Error("expected BuildDate to be set")
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("expected GoVersion=%s, got %s", runtime.Version(), info.GoVersion)
	}
	expectedPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != expectedPlatform {
		t.Errorf("expected Platform=%s, got %s", expectedPlatform, info.Platform)
	}
}

func TestInfo_String(t *testing.T) {
	info := Info{
		Version:   "v1.0.0",
		GitCommit: "abc1234",
		BuildDate: "2024-01-01T00:00:00Z",
		GoVersion: "go1.21.0",
		Platform:  "linux/amd64",
	}

	str := info.String()

	expectedParts := []string{
		"Version: v1.0.0",
		"Git Commit: abc1234",
		"Build Date: 2024-01-01T00:00:00Z",
		"Go Version: go1.21.0",
		"Platform: linux/amd64",
	}

	for _, part := range expectedParts {
		if !strings.Contains(str, part) {
			t.Errorf("expected string to contain %q, got %q", part, str)
		}
	}
}

func TestInfo_Short(t *testing.T) {
	info := Info{
		Version:   "v1.0.0",
		GitCommit: "abc1234567890",
	}

	short := info.Short()

	if !strings.Contains(short, "v1.0.0") {
		t.Errorf("expected short to contain version, got %q", short)
	}
	// Should truncate commit to 7 chars
	if !strings.Contains(short, "abc1234") {
		t.Errorf("expected short to contain truncated commit, got %q", short)
	}
}
