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

package app

import (
	"flag"
	"testing"
)

func TestNewOptions(t *testing.T) {
	opts := NewOptions()

	if opts.Port != DefaultPort {
		t.Errorf("expected Port=%d, got %d", DefaultPort, opts.Port)
	}
	if opts.NodeID != DefaultNodeID {
		t.Errorf("expected NodeID=%s, got %s", DefaultNodeID, opts.NodeID)
	}
	if opts.Cluster != DefaultCluster {
		t.Errorf("expected Cluster=%s, got %s", DefaultCluster, opts.Cluster)
	}
	if opts.MetricsAddr != DefaultMetricsAddr {
		t.Errorf("expected MetricsAddr=%s, got %s", DefaultMetricsAddr, opts.MetricsAddr)
	}
	if opts.ProbeAddr != DefaultProbeAddr {
		t.Errorf("expected ProbeAddr=%s, got %s", DefaultProbeAddr, opts.ProbeAddr)
	}
	if opts.EnvPrefix != DefaultEnvPrefix {
		t.Errorf("expected EnvPrefix=%s, got %s", DefaultEnvPrefix, opts.EnvPrefix)
	}
}

func TestOptions_Validate(t *testing.T) {
	tests := []struct {
		name    string
		opts    *Options
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid empty options",
			opts:    NewOptions(),
			wantErr: false,
		},
		{
			name: "valid email",
			opts: func() *Options {
				o := NewOptions()
				o.LetsEncryptEmail = "test@example.com"
				return o
			}(),
			wantErr: false,
		},
		{
			name: "invalid email",
			opts: func() *Options {
				o := NewOptions()
				o.LetsEncryptEmail = "invalid-email"
				return o
			}(),
			wantErr: true,
			errMsg:  "invalid Let's Encrypt email",
		},
		{
			name: "valid base64 private key",
			opts: func() *Options {
				o := NewOptions()
				o.LetsEncryptPrivateKeyBase64 = "dGVzdA==" // "test" in base64
				return o
			}(),
			wantErr: false,
		},
		{
			name: "invalid base64 private key",
			opts: func() *Options {
				o := NewOptions()
				o.LetsEncryptPrivateKeyBase64 = "not-valid-base64!!!"
				return o
			}(),
			wantErr: true,
			errMsg:  "invalid base64-encoded private key",
		},
		{
			name: "complete vault config",
			opts: func() *Options {
				o := NewOptions()
				o.VaultURL = "https://vault.example.com"
				o.VaultToken = "token123"
				o.VaultPath = "apps/envoy"
				return o
			}(),
			wantErr: false,
		},
		{
			name: "partial vault config - missing token",
			opts: func() *Options {
				o := NewOptions()
				o.VaultURL = "https://vault.example.com"
				o.VaultPath = "apps/envoy"
				return o
			}(),
			wantErr: true,
			errMsg:  "all Vault configuration",
		},
		{
			name: "partial vault config - missing path",
			opts: func() *Options {
				o := NewOptions()
				o.VaultURL = "https://vault.example.com"
				o.VaultToken = "token123"
				return o
			}(),
			wantErr: true,
			errMsg:  "all Vault configuration",
		},
		{
			name: "partial vault config - missing url",
			opts: func() *Options {
				o := NewOptions()
				o.VaultToken = "token123"
				o.VaultPath = "apps/envoy"
				return o
			}(),
			wantErr: true,
			errMsg:  "all Vault configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestOptions_BuildXDSConfig(t *testing.T) {
	opts := NewOptions()
	opts.NodeID = "test-node"
	opts.Cluster = "test-cluster"
	opts.DryRun = true
	opts.RenewBeforeExpireInMinutes = 1000

	config, err := opts.BuildXDSConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.NodeID != "test-node" {
		t.Errorf("expected NodeID=test-node, got %s", config.NodeID)
	}
	if config.Cluster != "test-cluster" {
		t.Errorf("expected Cluster=test-cluster, got %s", config.Cluster)
	}
	if !config.DryRun {
		t.Error("expected DryRun=true")
	}
	if config.ReconciliationStatus == nil {
		t.Error("expected ReconciliationStatus to be initialized")
	}
	if config.HTTPClient == nil {
		t.Error("expected HttpClient to be initialized")
	}
	// RndNumber should be between 1 and 360
	if config.RndNumber < 1 || config.RndNumber > 360 {
		t.Errorf("expected RndNumber between 1 and 360, got %d", config.RndNumber)
	}
}

func TestOptions_BuildXDSConfig_WithLetsEncrypt(t *testing.T) {
	opts := NewOptions()
	opts.LetsEncryptEmail = "test@example.com"
	opts.LetsEncryptPrivateKeyBase64 = "dGVzdA==" // "test" in base64

	config, err := opts.BuildXDSConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.LetsEncryptAccount.Email != "test@example.com" {
		t.Errorf("expected Email=test@example.com, got %s", config.LetsEncryptAccount.Email)
	}
	if config.LetsEncryptAccount.PrivateKeyBase64 != "dGVzdA==" {
		t.Errorf("expected PrivateKeyBase64=dGVzdA==, got %s", config.LetsEncryptAccount.PrivateKeyBase64)
	}
}

func TestOptions_BuildXDSConfig_WithVault(t *testing.T) {
	opts := NewOptions()
	opts.VaultURL = "https://vault.example.com"
	opts.VaultToken = "token123"
	opts.VaultPath = "apps/envoy"
	opts.VaultSkipTLSVerify = true

	config, err := opts.BuildXDSConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.Storage.VaultStorageConfig == nil {
		t.Fatal("expected VaultStorageConfig to be set")
	}
	if config.Storage.VaultStorageConfig.URL != "https://vault.example.com" {
		t.Errorf("expected URL=https://vault.example.com, got %s", config.Storage.VaultStorageConfig.URL)
	}
	if config.Storage.VaultStorageConfig.Token != "token123" {
		t.Errorf("expected Token=token123, got %s", config.Storage.VaultStorageConfig.Token)
	}
	if config.Storage.VaultStorageConfig.Path != "apps/envoy" {
		t.Errorf("expected Path=apps/envoy, got %s", config.Storage.VaultStorageConfig.Path)
	}
	if !config.Storage.VaultStorageConfig.SkipTLSVerify {
		t.Error("expected SkipTLSVerify=true")
	}
}

func TestOptions_RegisterFlags(t *testing.T) {
	opts := NewOptions()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.RegisterFlags(fs)

	// Parse with custom values
	err := fs.Parse([]string{
		"-port", "19000",
		"-nodeID", "custom-node",
		"-cluster", "custom-cluster",
		"-namespace", "custom-ns",
		"-dry-run",
	})
	if err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	if opts.Port != 19000 {
		t.Errorf("expected Port=19000, got %d", opts.Port)
	}
	if opts.NodeID != "custom-node" {
		t.Errorf("expected NodeID=custom-node, got %s", opts.NodeID)
	}
	if opts.Cluster != "custom-cluster" {
		t.Errorf("expected Cluster=custom-cluster, got %s", opts.Cluster)
	}
	if opts.Namespace != "custom-ns" {
		t.Errorf("expected Namespace=custom-ns, got %s", opts.Namespace)
	}
	if !opts.DryRun {
		t.Error("expected DryRun=true")
	}
}

func TestOptions_LoadFromEnvironment(t *testing.T) {
	// Set test environment (t.Setenv auto-restores original values)
	t.Setenv("XDS_LETS_ENCRYPT_EMAIL", "env@example.com")
	t.Setenv("XDS_LOG_LEVEL", "2")
	t.Setenv("XDS_PRODUCTION", "true")

	opts := NewOptions()
	opts.LoadFromEnvironment()

	if opts.LetsEncryptEmail != "env@example.com" {
		t.Errorf("expected LetsEncryptEmail=env@example.com, got %s", opts.LetsEncryptEmail)
	}
	if opts.LogLevel != 2 {
		t.Errorf("expected LogLevel=2, got %d", opts.LogLevel)
	}
	if !opts.Production {
		t.Error("expected Production=true")
	}
}

// contains checks if substr is in s
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
