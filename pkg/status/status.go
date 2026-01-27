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

// Package status provides reconciliation status tracking for the xDS controller.
// It tracks the state of various resource types during the reconciliation process.
package status

import (
	"sync/atomic"
)

// ReconciliationStatus tracks the reconciliation status of various components.
type ReconciliationStatus struct {
	listenersReconciled     atomic.Bool
	routesReconciled        atomic.Bool
	clustersReconciled      atomic.Bool
	endpointsReconciled     atomic.Bool
	domainConfigsReconciled atomic.Bool
	snapshotGenerated       atomic.Bool

	// Track if we've seen any resources (if none, we're ready immediately)
	hasListeners     atomic.Bool
	hasRoutes        atomic.Bool
	hasClusters      atomic.Bool
	hasEndpoints     atomic.Bool
	hasDomainConfigs atomic.Bool
}

// NewReconciliationStatus creates a new ReconciliationStatus.
func NewReconciliationStatus() *ReconciliationStatus {
	rs := &ReconciliationStatus{}
	// By default, mark all as reconciled (nothing to do = done)
	// These will be set to false when resources are discovered
	rs.listenersReconciled.Store(true)
	rs.routesReconciled.Store(true)
	rs.clustersReconciled.Store(true)
	rs.endpointsReconciled.Store(true)
	rs.domainConfigsReconciled.Store(true)
	return rs
}

// IsListenersReconciled checks if listeners have been reconciled.
func (rs *ReconciliationStatus) IsListenersReconciled() bool {
	val := rs.listenersReconciled.Load()
	return val
}

// SetListenersReconciled sets the listeners reconciled status.
func (rs *ReconciliationStatus) SetListenersReconciled(reconciled bool) {
	rs.listenersReconciled.Store(reconciled)
}

// IsRoutesReconciled checks if routes have been reconciled.
func (rs *ReconciliationStatus) IsRoutesReconciled() bool {
	val := rs.routesReconciled.Load()
	return val
}

// SetRoutesReconciled sets the routes reconciled status.
func (rs *ReconciliationStatus) SetRoutesReconciled(reconciled bool) {
	rs.routesReconciled.Store(reconciled)
}

// IsClustersReconciled checks if clusters have been reconciled.
func (rs *ReconciliationStatus) IsClustersReconciled() bool {
	val := rs.clustersReconciled.Load()
	return val
}

// SetClustersReconciled sets the clusters reconciled status.
func (rs *ReconciliationStatus) SetClustersReconciled(reconciled bool) {
	rs.clustersReconciled.Store(reconciled)
}

// IsEndpointsReconciled checks if endpoints have been reconciled.
func (rs *ReconciliationStatus) IsEndpointsReconciled() bool {
	val := rs.endpointsReconciled.Load()
	return val
}

// SetEndpointsReconciled sets the endpoints reconciled status.
func (rs *ReconciliationStatus) SetEndpointsReconciled(reconciled bool) {
	rs.endpointsReconciled.Store(reconciled)
}

// IsDomainConfigsReconciled checks if domain configurations have been reconciled.
func (rs *ReconciliationStatus) IsDomainConfigsReconciled() bool {
	val := rs.domainConfigsReconciled.Load()
	return val
}

// SetDomainConfigsReconciled sets the domain configs reconciled status.
func (rs *ReconciliationStatus) SetDomainConfigsReconciled(reconciled bool) {
	rs.domainConfigsReconciled.Store(reconciled)
}

// IsSnapshotGenerated checks if a snapshot has been generated.
func (rs *ReconciliationStatus) IsSnapshotGenerated() bool {
	val := rs.snapshotGenerated.Load()
	return val
}

// SetSnapshotGenerated sets the snapshot generated status.
func (rs *ReconciliationStatus) SetSnapshotGenerated(generated bool) {
	rs.snapshotGenerated.Store(generated)
}

// SetHasListeners marks that we have listeners to reconcile
func (rs *ReconciliationStatus) SetHasListeners(has bool) {
	rs.hasListeners.Store(has)
	if has {
		// Reset reconciled flag when new resources appear
		rs.listenersReconciled.Store(false)
	}
}

// SetHasRoutes marks that we have routes to reconcile
func (rs *ReconciliationStatus) SetHasRoutes(has bool) {
	rs.hasRoutes.Store(has)
	if has {
		rs.routesReconciled.Store(false)
	}
}

// SetHasClusters marks that we have clusters to reconcile
func (rs *ReconciliationStatus) SetHasClusters(has bool) {
	rs.hasClusters.Store(has)
	if has {
		rs.clustersReconciled.Store(false)
	}
}

// SetHasEndpoints marks that we have endpoints to reconcile
func (rs *ReconciliationStatus) SetHasEndpoints(has bool) {
	rs.hasEndpoints.Store(has)
	if has {
		rs.endpointsReconciled.Store(false)
	}
}

// SetHasDomainConfigs marks that we have domain configs to reconcile
func (rs *ReconciliationStatus) SetHasDomainConfigs(has bool) {
	rs.hasDomainConfigs.Store(has)
	if has {
		rs.domainConfigsReconciled.Store(false)
	}
}

// HasAnyResources checks if any resources have been discovered
func (rs *ReconciliationStatus) HasAnyResources() bool {
	return rs.hasListeners.Load() ||
		rs.hasRoutes.Load() ||
		rs.hasClusters.Load() ||
		rs.hasEndpoints.Load() ||
		rs.hasDomainConfigs.Load()
}

// AllReconciledWithSnapshot checks if all components have been reconciled and a snapshot has been generated.
func (rs *ReconciliationStatus) AllReconciledWithSnapshot() bool {
	// If there are no resources, we're ready immediately
	if !rs.HasAnyResources() {
		return true
	}

	if rs.IsSnapshotGenerated() {
		return true
	}
	return rs.AllReconciled()
}

// AllReconciled checks if all components have been reconciled.
func (rs *ReconciliationStatus) AllReconciled() bool {
	// For each resource type, either we don't have any OR they've been reconciled
	listenersOK := !rs.hasListeners.Load() || rs.IsListenersReconciled()
	routesOK := !rs.hasRoutes.Load() || rs.IsRoutesReconciled()
	clustersOK := !rs.hasClusters.Load() || rs.IsClustersReconciled()
	endpointsOK := !rs.hasEndpoints.Load() || rs.IsEndpointsReconciled()
	domainConfigsOK := !rs.hasDomainConfigs.Load() || rs.IsDomainConfigsReconciled()

	return listenersOK && routesOK && clustersOK && endpointsOK && domainConfigsOK
}
