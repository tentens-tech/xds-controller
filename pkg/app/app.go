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

// Package app provides the main application logic for the Envoy XDS Controller.
// It handles configuration loading, controller setup, and the main run loop.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Required for cloud provider authentication (GCP, Azure, etc.)
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/pkg/leader"
	"github.com/tentens-tech/xds-controller/pkg/serv"
	"github.com/tentens-tech/xds-controller/pkg/version"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(envoyxdsv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// Run is the main entry point for the Envoy XDS Controller.
// It loads configuration, sets up controllers, and starts the manager.
func Run(ctx context.Context, opts *Options) error {
	log := ctrl.Log.WithName("xDS")

	// Validate configuration
	if err := opts.Validate(); err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Resolve namespace if not specified
	opts.ResolveNamespace()

	// Build XDS configuration
	xdsConfig, err := opts.BuildXDSConfig()
	if err != nil {
		return fmt.Errorf("xds config error: %w", err)
	}

	// Create snapshot cache for xDS
	snapshotCache := cache.NewSnapshotCache(false, cache.IDHash{}, nil)

	// Initialize xDS gRPC server
	server := serv.ServerInit(opts.Port, snapshotCache)
	server.Config = xdsConfig

	// Create controller manager
	mgr, err := createManager(opts)
	if err != nil {
		return fmt.Errorf("manager creation error: %w", err)
	}

	// Set up the Kubernetes client on the xds config for SDS storage
	xdsConfig.K8sClient = mgr.GetClient()
	xdsConfig.DefaultNamespace = opts.Namespace

	// Setup controllers
	if setupErr := SetupControllers(mgr, xdsConfig); setupErr != nil {
		return fmt.Errorf("controller setup error: %w", setupErr)
	}

	// Add xDS server to manager (implements Runnable interface)
	if addErr := mgr.Add(server); addErr != nil {
		return fmt.Errorf("failed to add xDS server to manager: %w", addErr)
	}

	// Setup health checks
	if healthErr := SetupHealthChecks(mgr, xdsConfig.ReconciliationStatus); healthErr != nil {
		return fmt.Errorf("health check setup error: %w", healthErr)
	}

	// Create and start leader election
	leaderMgr, err := createLeaderManager(opts, xdsConfig)
	if err != nil {
		return fmt.Errorf("leader election setup error: %w", err)
	}

	// Start leader election in background
	go func() {
		if err := leaderMgr.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error(err, "leader election failed")
		}
	}()

	// Wait for initial leader election to complete
	if err := leaderMgr.WaitForInitialElection(ctx); err != nil {
		return fmt.Errorf("initial leader election failed: %w", err)
	}

	// Start manager (blocks until context is canceled)
	log.Info("Starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("manager error: %w", err)
	}

	// Graceful shutdown: release leadership
	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer releaseCancel()

	if err := leaderMgr.ReleaseLeadership(releaseCtx); err != nil {
		log.Error(err, "failed to release leadership during shutdown")
	} else {
		log.V(0).Info("Successfully released leadership")
	}

	return nil
}

// RunWithFlags parses flags, loads environment configuration, and runs the controller.
// This is a convenience function for the main package.
func RunWithFlags(ctx context.Context) error {
	// Version flag
	showVersion := flag.Bool("version", false, "Print version information and exit")

	opts := NewOptions()
	opts.RegisterFlags(flag.CommandLine)

	// Load from environment before parsing flags
	// (env vars set defaults, flags override)
	opts.LoadFromEnvironment()

	// Setup logger and bind its flags
	logger := SetupLogger(opts, flag.CommandLine)

	// Parse all flags
	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Println(version.Get().String())
		os.Exit(0)
	}

	// Set the global logger
	ctrl.SetLogger(logger)

	// Log version info on startup
	versionInfo := version.Get()
	ctrl.Log.WithName("xDS").Info("Starting Envoy XDS Controller",
		"version", versionInfo.Version,
		"commit", versionInfo.GitCommit,
		"buildDate", versionInfo.BuildDate,
	)

	return Run(ctx, opts)
}

// createManager creates the controller-runtime manager with the given options.
func createManager(opts *Options) (ctrl.Manager, error) {
	mgrOpts := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: opts.MetricsAddr},
		HealthProbeBindAddress: opts.ProbeAddr,
	}

	// Configure namespace-scoped cache if namespace is specified
	if opts.Namespace != "" {
		mgrOpts.Cache = ctrlcache.Options{
			DefaultNamespaces: map[string]ctrlcache.Config{
				opts.Namespace: {},
			},
		}
	}

	return ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
}

// createLeaderManager creates and configures the leader election manager.
func createLeaderManager(opts *Options, xdsConfig interface{ SetLeaderStatus(bool) }) (*leader.Manager, error) {
	electionConfig := &leader.ElectionConfig{
		LeaseDuration: LeaderLeaseDuration,
		RenewDeadline: LeaderRenewDeadline,
		RetryPeriod:   LeaderRetryPeriod,
		Namespace:     opts.Namespace,
		LockName:      LeaderLockName,
	}

	return leader.NewManager(electionConfig, xdsConfig.SetLeaderStatus)
}
