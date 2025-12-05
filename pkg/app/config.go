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
	"crypto/tls"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
	xdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types"
)

// Default configuration values.
const (
	DefaultPort                    = 18000
	DefaultNodeID                  = "global"
	DefaultCluster                 = "global"
	DefaultRefreshTime             = 5000 * time.Millisecond
	DefaultRenewBeforeExpireInMins = 43200
	DefaultEnvFilePath             = "/vault/secrets/env"
	DefaultMetricsAddr             = ":8080"
	DefaultProbeAddr               = ":8081"
	DefaultEnvPrefix               = "XDS_"

	// Leader election configuration.
	LeaderLeaseDuration = 15 * time.Second
	LeaderRenewDeadline = 10 * time.Second
	LeaderRetryPeriod   = 2 * time.Second
	LeaderLockName      = "envoy-xds-controller-lock"

	// ShutdownTimeout for releasing leadership.
	ShutdownTimeout = 10 * time.Second
)

// Options holds all configuration options for the controller.
type Options struct {
	// Server configuration
	Port      uint
	NodeID    string
	Cluster   string
	Namespace string

	// Kubernetes controller configuration
	MetricsAddr string
	ProbeAddr   string

	// Certificate renewal
	RenewBeforeExpireInMinutes int

	// Let's Encrypt configuration
	LetsEncryptEmail            string
	LetsEncryptPrivateKeyBase64 string

	// Vault configuration
	VaultURL           string
	VaultToken         string
	VaultPath          string
	VaultSkipTLSVerify bool

	// Operational flags
	DryRun    bool
	EnvPrefix string

	// Logging
	LogLevel   int
	Production bool
}

// NewOptions creates Options with default values.
func NewOptions() *Options {
	return &Options{
		Port:                       DefaultPort,
		NodeID:                     DefaultNodeID,
		Cluster:                    DefaultCluster,
		MetricsAddr:                DefaultMetricsAddr,
		ProbeAddr:                  DefaultProbeAddr,
		RenewBeforeExpireInMinutes: DefaultRenewBeforeExpireInMins,
		EnvPrefix:                  DefaultEnvPrefix,
		LogLevel:                   -1, // Default zap debug level
	}
}

// RegisterFlags registers command line flags for the options.
func (o *Options) RegisterFlags(fs *flag.FlagSet) {
	// Server configuration
	fs.UintVar(&o.Port, "port", o.Port, "xDS management server port")
	fs.StringVar(&o.NodeID, "nodeID", o.NodeID, "Default Envoy Node ID")
	fs.StringVar(&o.Cluster, "cluster", o.Cluster, "Default Envoy Cluster")
	fs.StringVar(&o.Namespace, "namespace", o.Namespace, "Namespace to watch (defaults to current namespace)")

	// Let's Encrypt configuration
	fs.StringVar(&o.LetsEncryptEmail, "email", o.LetsEncryptEmail, "Email for Let's Encrypt notifications")
	fs.StringVar(&o.LetsEncryptPrivateKeyBase64, "privateKeyBase64", o.LetsEncryptPrivateKeyBase64,
		"Base64-encoded private key for Let's Encrypt registration")

	// Vault configuration
	fs.StringVar(&o.VaultURL, "vaultURL", o.VaultURL, "Vault server URL")
	fs.StringVar(&o.VaultToken, "vaultToken", o.VaultToken, "Vault authentication token")
	fs.StringVar(&o.VaultPath, "vaultPath", o.VaultPath, "Vault KV2 path (e.g., apps/envoy)")
	fs.BoolVar(&o.VaultSkipTLSVerify, "vault-skip-tls-verify", o.VaultSkipTLSVerify,
		"Skip TLS verification for Vault connection")

	// Certificate renewal
	fs.IntVar(&o.RenewBeforeExpireInMinutes, "renewBeforeExpireInMinutes", o.RenewBeforeExpireInMinutes,
		"Renew certificates when remaining validity is less than this value (in minutes)")

	// Kubernetes controller configuration
	fs.StringVar(&o.MetricsAddr, "metrics-bind-address", o.MetricsAddr, "Address for the metrics endpoint")
	fs.StringVar(&o.ProbeAddr, "health-probe-bind-address", o.ProbeAddr, "Address for the health probe endpoint")

	// Operational flags
	fs.BoolVar(&o.DryRun, "dry-run", o.DryRun, "Read-only mode: no write operations will be performed")
	fs.StringVar(&o.EnvPrefix, "env-prefix", o.EnvPrefix, "Prefix for environment variables")
}

// LoadFromEnvironment loads configuration from environment variables.
// Environment variables override flag values only if the flag value is empty/default.
func (o *Options) LoadFromEnvironment() {
	// Load environment file
	envFilePath := DefaultEnvFilePath
	if path := os.Getenv(o.EnvPrefix + "ENV_FILE"); path != "" {
		envFilePath = path
	}
	loadEnvFromFile(envFilePath)

	// Load from environment variables
	loadEnvString(&o.LetsEncryptEmail, o.EnvPrefix+"LETS_ENCRYPT_EMAIL")
	loadEnvString(&o.LetsEncryptPrivateKeyBase64, o.EnvPrefix+"LETS_ENCRYPT_PRIVATEKEYB64")
	loadEnvString(&o.VaultURL, o.EnvPrefix+"VAULT_URL")
	loadEnvString(&o.VaultToken, o.EnvPrefix+"VAULT_TOKEN")
	loadEnvString(&o.VaultPath, o.EnvPrefix+"VAULT_PATH")
	loadEnvString(&o.Namespace, o.EnvPrefix+"NAMESPACE")

	// Load log level
	if envLogLevel := os.Getenv(o.EnvPrefix + "LOG_LEVEL"); envLogLevel != "" {
		if level, err := strconv.Atoi(envLogLevel); err == nil {
			o.LogLevel = level
		}
	}

	// Load production mode
	if envProduction := os.Getenv(o.EnvPrefix + "PRODUCTION"); envProduction != "" {
		if val, err := strconv.ParseBool(envProduction); err == nil {
			o.Production = val
		}
	}
}

// Validate validates the configuration options.
func (o *Options) Validate() error {
	// Validate Let's Encrypt email
	if o.LetsEncryptEmail != "" {
		if _, err := mail.ParseAddress(o.LetsEncryptEmail); err != nil {
			return fmt.Errorf("invalid Let's Encrypt email '%s': %w", o.LetsEncryptEmail, err)
		}
	}

	// Validate Let's Encrypt private key
	if o.LetsEncryptPrivateKeyBase64 != "" {
		if _, err := base64.StdEncoding.DecodeString(o.LetsEncryptPrivateKeyBase64); err != nil {
			return fmt.Errorf("invalid base64-encoded private key: %w", err)
		}
	}

	// Validate Vault configuration - all or nothing
	hasVaultConfig := o.VaultURL != "" || o.VaultToken != "" || o.VaultPath != ""
	hasCompleteVaultConfig := o.VaultURL != "" && o.VaultToken != "" && o.VaultPath != ""
	if hasVaultConfig && !hasCompleteVaultConfig {
		return errors.New("all Vault configuration (URL, token, path) must be provided together")
	}

	return nil
}

// ResolveNamespace determines the namespace to watch.
// It checks (in order): configured value, service account, kubeconfig, then defaults to "default".
func (o *Options) ResolveNamespace() {
	log := ctrl.Log.WithName("config")

	if o.Namespace != "" {
		log.V(0).Info("Using configured namespace", "namespace", o.Namespace)
		return
	}

	// Try to detect from Kubernetes service account (in-cluster)
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
			o.Namespace = string(data)
			log.V(0).Info("Detected namespace from service account", "namespace", o.Namespace)
			return
		}
	}

	// Try kubeconfig
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)
	if ns, _, err := kubeconfig.Namespace(); err == nil && ns != "" {
		o.Namespace = ns
		log.V(0).Info("Detected namespace from kubeconfig", "namespace", o.Namespace)
		return
	}

	// Fall back to default
	o.Namespace = "default"
	log.V(0).Info("Using default namespace", "namespace", o.Namespace)
}

// BuildXDSConfig creates the XDS configuration from the options.
func (o *Options) BuildXDSConfig() (*xds.Config, error) {
	config := &xds.Config{
		NodeID:                     o.NodeID,
		Cluster:                    o.Cluster,
		RefreshTime:                DefaultRefreshTime,
		RenewBeforeExpireInMinutes: o.RenewBeforeExpireInMinutes,
		DryRun:                     o.DryRun,
		ReconciliationStatus:       status.NewReconciliationStatus(),
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: o.VaultSkipTLSVerify}, //nolint:gosec // TLS verification skip is user-configurable for development environments
			},
		},
	}

	// Set Let's Encrypt configuration
	if o.LetsEncryptEmail != "" {
		if email, err := mail.ParseAddress(o.LetsEncryptEmail); err == nil {
			config.LetsEncryptAccount.Email = email.Address
		}
	}
	if o.LetsEncryptPrivateKeyBase64 != "" {
		config.LetsEncryptAccount.PrivateKeyBase64 = o.LetsEncryptPrivateKeyBase64
	}

	// Set Vault configuration
	if o.VaultURL != "" && o.VaultToken != "" && o.VaultPath != "" {
		config.Storage.VaultStorageConfig = &xdstypes.VaultStorageConfig{
			URL:           o.VaultURL,
			Token:         o.VaultToken,
			Path:          o.VaultPath,
			SkipTLSVerify: o.VaultSkipTLSVerify,
		}
	}

	// Add randomization for certificate renewal to prevent thundering herd
	config.RndNumber = rand.IntN(360) + 1 // #nosec G404 -- non-cryptographic random for load distribution
	config.RenewBeforeExpireInMinutes -= config.RndNumber

	return config, nil
}

// loadEnvString loads an environment variable into a string pointer if the pointer is empty.
func loadEnvString(target *string, envName string) {
	if *target == "" {
		*target = os.Getenv(envName)
	}
}

// loadEnvFromFile loads environment variables from a file if it exists.
func loadEnvFromFile(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}

	if err := godotenv.Load(path); err != nil {
		ctrl.Log.WithName("config").V(0).Info("Failed to load environment file", "path", path, "error", err)
	}
}
