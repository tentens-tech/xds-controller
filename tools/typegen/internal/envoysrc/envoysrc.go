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

// Package envoysrc reads go-control-plane sources from the module cache, at the
// version go.mod selects, so generated types match the compiled protos.
package envoysrc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const modulePath = "github.com/envoyproxy/go-control-plane/envoy"

// Source is the go-control-plane envoy module on disk.
type Source struct {
	Dir     string
	Version string
}

// Locate finds the envoy module selected by go.mod.
func Locate() (Source, error) {
	out, err := exec.CommandContext(context.Background(), "go", "list", "-m", "-f", "{{.Dir}}\t{{.Version}}", modulePath).Output()
	if err != nil {
		return Source{}, fmt.Errorf("locating %s: %w", modulePath, err)
	}
	dir, version, _ := strings.Cut(strings.TrimRight(string(out), "\r\n"), "\t")
	if dir == "" {
		return Source{}, fmt.Errorf("%s is not in the module cache; run go mod download", modulePath)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Source{}, fmt.Errorf("%s directory %q is missing; run go mod download", modulePath, dir)
	}
	return Source{Dir: dir, Version: version}, nil
}

// File is one generated .pb.go file of a package.
type File struct {
	Name    string
	Content []byte
}

// PackageFiles returns the .pb.go files of pkgPath (e.g. "envoy/config/core/v3") in name order.
func (s Source) PackageFiles(pkgPath string) ([]File, error) {
	dir := filepath.Join(s.Dir, strings.TrimPrefix(pkgPath, "envoy/"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading package %s: %w", pkgPath, err)
	}
	var files []File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".pb.go") || strings.HasSuffix(name, "_vtproto.pb.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- path is inside the module cache
		if err != nil {
			return nil, err
		}
		files = append(files, File{Name: name, Content: content})
	}
	return files, nil
}
