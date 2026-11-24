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
	"errors"
	"fmt"
	"io/fs"
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
	dir, version, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
	if dir == "" {
		return Source{}, fmt.Errorf("%s is not in the module cache; run go mod download", modulePath)
	}
	return Source{Dir: dir, Version: version}, nil
}

// ReadPackageFile reads name from pkgPath (e.g. "envoy/config/core/v3").
// It returns nil content without error when the file does not exist.
func (s Source) ReadPackageFile(pkgPath, name string) ([]byte, error) {
	path := filepath.Join(s.Dir, strings.TrimPrefix(pkgPath, "envoy/"), name)
	content, err := os.ReadFile(path) // #nosec G304 -- path is inside the module cache
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return content, err
}
