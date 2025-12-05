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

// Package err defines common error types used throughout the xDS controller.
package err

import "errors"

var (
	ErrDomainConfigNotFound   = errors.New("domain configs not found, waiting")
	ErrManualCertNotFound     = errors.New("certificate not found and is manual, skipping %s")
	ErrGetCert                = errors.New("get cert error: %s")
	ErrReadCert               = errors.New("read cert error: %s")
	ErrWriteCert              = errors.New("write cert error: %s")
	ErrBadKeyData             = errors.New("bad key data: not PEM-encoded")
	ErrCertType               = errors.New("ERROR: cert type: %s")
	ErrCertNotFound           = errors.New("cert not found")
	ErrCertNil                = errors.New("cert config is nil")
	ErrVaultNotConfigured     = errors.New("vault not configured")
	ErrK8sClientNotConfigured = errors.New("kubernetes client not configured")
	ErrCustomEnvReplace       = errors.New("not even number of envs to replace")
	ErrNoResourcesFound       = errors.New("no resources found")
	ErrServiceBusy            = errors.New("service busy")
	ErrRateLimited            = errors.New("rate limited")
)
