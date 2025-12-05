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

// Package version provides version information for the Envoy XDS Controller.
// Version values are typically injected at build time using ldflags.
package version

import (
	"fmt"
	"runtime"
)

// These variables are set at build time using ldflags.
// Example: go build -ldflags "-X gitlab.com/.../pkg/version.Version=v1.0.0"
var (
	// Version is the semantic version of the build.
	Version = "dev"

	// GitCommit is the git commit hash of the build.
	GitCommit = "unknown"

	// BuildDate is the date when the binary was built.
	BuildDate = "unknown"
)

// Info contains version information.
type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"gitCommit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Get returns the version information.
func Get() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String returns a human-readable version string.
func (i Info) String() string {
	return fmt.Sprintf("Version: %s\nGit Commit: %s\nBuild Date: %s\nGo Version: %s\nPlatform: %s",
		i.Version, i.GitCommit, i.BuildDate, i.GoVersion, i.Platform)
}

// Short returns a short version string suitable for logs.
func (i Info) Short() string {
	return fmt.Sprintf("%s (commit: %.7s)", i.Version, i.GitCommit)
}
