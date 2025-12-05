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
	"errors"
	"fmt"
	"net/http"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/tentens-tech/xds-controller/controllers/cds"
	"github.com/tentens-tech/xds-controller/controllers/eds"
	"github.com/tentens-tech/xds-controller/controllers/lds"
	"github.com/tentens-tech/xds-controller/controllers/rds"
	"github.com/tentens-tech/xds-controller/controllers/sds"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
)

// controllerDefinition defines a controller to be registered.
type controllerDefinition struct {
	name  string
	setup func(mgr ctrl.Manager, config *xds.Config) error
}

// SetupControllers registers all xDS controllers with the manager.
func SetupControllers(mgr ctrl.Manager, config *xds.Config) error {
	log := ctrl.Log.WithName("setup")

	controllers := []controllerDefinition{
		{
			name: "Cluster",
			setup: func(mgr ctrl.Manager, config *xds.Config) error {
				return (&cds.ClusterReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Config: config,
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "Endpoint",
			setup: func(mgr ctrl.Manager, config *xds.Config) error {
				return (&eds.EndpointReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Config: config,
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "Route",
			setup: func(mgr ctrl.Manager, config *xds.Config) error {
				return (&rds.RouteReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Config: config,
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "Listener",
			setup: func(mgr ctrl.Manager, config *xds.Config) error {
				return (&lds.ListenerReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Config: config,
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "TLSSecret",
			setup: func(mgr ctrl.Manager, config *xds.Config) error {
				return (&sds.TLSSecretReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Config: config,
				}).SetupWithManager(mgr)
			},
		},
	}

	for _, c := range controllers {
		if err := c.setup(mgr, config); err != nil {
			return fmt.Errorf("unable to create %s controller: %w", c.name, err)
		}
		log.V(1).Info("Controller registered", "controller", c.name)
	}

	return nil
}

// SetupHealthChecks configures health and readiness probes for the manager.
func SetupHealthChecks(mgr ctrl.Manager, rs *status.ReconciliationStatus) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	if err := mgr.AddReadyzCheck("readyz", ReadinessCheck(rs)); err != nil {
		return fmt.Errorf("unable to set up readiness check: %w", err)
	}

	return nil
}

// ReadinessCheck returns a health check function that verifies all controllers have reconciled.
func ReadinessCheck(rs *status.ReconciliationStatus) healthz.Checker {
	return func(req *http.Request) error {
		if !rs.AllReconciledWithSnapshot() {
			return errors.New("not all controllers have reconciled yet")
		}
		return nil
	}
}
