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

// Package leader provides leader election functionality for the xDS controller.
// It uses Kubernetes Lease objects for distributed leader election.
package leader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ElectionConfig contains configuration for leader election
type ElectionConfig struct {
	// LeaseDuration is the duration that non-leader candidates will
	// wait to force acquire leadership
	LeaseDuration time.Duration
	// RenewDeadline is the duration that the acting leader will retry
	// refreshing leadership before giving up
	RenewDeadline time.Duration
	// RetryPeriod is the duration the LeaderElector clients should wait
	// between tries of actions
	RetryPeriod time.Duration
	// Namespace where the leader election configmap will be created
	Namespace string
	// LockName is the name of configmap that is used for holding the leader lock
	LockName string
}

// Manager manages leader election
type Manager struct {
	config    *ElectionConfig
	clientset *kubernetes.Clientset
	isLeader  bool
	stopCh    chan struct{}
	// Callback function when leader status changes
	onLeadershipChange func(bool)
	// The actual node identifier being used
	nodeIdentifier string
	// Channel to signal when initial election is done
	initialElectionDone chan struct{}
}

// getNodeIdentifier returns the appropriate node identifier based on the configuration
func (m *Manager) getNodeIdentifier() (string, error) {
	// First try POD_NAME
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		return podName, nil
	}

	// Fall back to hostname if POD_NAME is not set
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("failed to get hostname: %w", err)
	}
	return hostname, nil
}

// NewManager creates a new leader election manager
func NewManager(config *ElectionConfig, onLeadershipChange func(bool)) (*Manager, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Use config namespace if set, otherwise try to detect it
	if config.Namespace == "" {
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{},
		)
		namespace, _, err := clientConfig.Namespace()
		if err != nil || namespace == "" {
			namespace = "default"
		}
		config.Namespace = namespace
	}

	clientset, err := kubernetes.NewForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	manager := &Manager{
		config:              config,
		clientset:           clientset,
		stopCh:              make(chan struct{}),
		onLeadershipChange:  onLeadershipChange,
		initialElectionDone: make(chan struct{}),
	}

	// Get the node identifier
	nodeID, err := manager.getNodeIdentifier()
	if err != nil {
		return nil, fmt.Errorf("failed to get node identifier: %w", err)
	}
	manager.nodeIdentifier = nodeID

	return manager, nil
}

// WaitForInitialElection blocks until the initial leader election is complete
func (m *Manager) WaitForInitialElection(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.initialElectionDone:
		return nil
	}
}

// Start begins the leader election process
func (m *Manager) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)
	ticker := time.NewTicker(m.config.RetryPeriod)
	defer ticker.Stop()
	initialElection := true
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-m.stopCh:
			return nil
		case <-ticker.C:
			lease, err := m.clientset.CoordinationV1().Leases(m.config.Namespace).Get(ctx, m.config.LockName, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					// Try to create new lease and become leader
					lease = &coordinationv1.Lease{
						ObjectMeta: metav1.ObjectMeta{
							Name:      m.config.LockName,
							Namespace: m.config.Namespace,
						},
						Spec: coordinationv1.LeaseSpec{
							HolderIdentity:       &m.nodeIdentifier,
							RenewTime:            &metav1.MicroTime{Time: time.Now()},
							LeaseDurationSeconds: ptr.To(int32(m.config.LeaseDuration.Seconds())),
						},
					}
					_, err = m.clientset.CoordinationV1().Leases(m.config.Namespace).Create(ctx, lease, metav1.CreateOptions{})
					if err != nil {
						if !errors.Is(err, context.Canceled) && !apierrors.IsAlreadyExists(err) {
							logger.Error(err, "Error creating lease")
						}
						if initialElection {
							close(m.initialElectionDone)
							initialElection = false
						}
						continue
					}
					logger.Info("Acquired leadership by creating lease", "node", m.nodeIdentifier)
					m.setLeaderStatus(true)
					if initialElection {
						close(m.initialElectionDone)
						initialElection = false
					}
					continue
				}
				if !errors.Is(err, context.Canceled) {
					logger.Error(err, "Error getting lease")
				}
				if initialElection {
					close(m.initialElectionDone)
					initialElection = false
				}
				continue
			}

			now := time.Now()
			leaseExpired := lease.Spec.RenewTime == nil ||
				now.Sub(lease.Spec.RenewTime.Time) > time.Duration(*lease.Spec.LeaseDurationSeconds)*time.Second

			switch {
			case lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == m.nodeIdentifier:
				// We're the current holder, renew the lease
				lease.Spec.RenewTime = &metav1.MicroTime{Time: now}
				_, err = m.clientset.CoordinationV1().Leases(m.config.Namespace).Update(ctx, lease, metav1.UpdateOptions{})
				if err != nil {
					if !errors.Is(err, context.Canceled) {
						logger.Error(err, "Error updating lease")
					}
					if m.isLeader {
						logger.Info("Lost leadership due to update failure", "node", m.nodeIdentifier)
						m.setLeaderStatus(false)
					}
					if initialElection {
						close(m.initialElectionDone)
						initialElection = false
					}
					continue
				}
				if !m.isLeader {
					logger.V(0).Info("Acquired leadership", "node", m.nodeIdentifier)
					m.setLeaderStatus(true)
				}
			case leaseExpired:
				// Try to take over expired lease
				lease.Spec.HolderIdentity = &m.nodeIdentifier
				lease.Spec.RenewTime = &metav1.MicroTime{Time: now}
				_, err = m.clientset.CoordinationV1().Leases(m.config.Namespace).Update(ctx, lease, metav1.UpdateOptions{})
				if err != nil {
					if !errors.Is(err, context.Canceled) {
						logger.Error(err, "Error taking over expired lease")
					}
					if initialElection {
						close(m.initialElectionDone)
						initialElection = false
					}
					continue
				}
				logger.V(0).Info("Acquired leadership by taking over expired lease", "node", m.nodeIdentifier)
				m.setLeaderStatus(true)
			case m.isLeader:
				// We're not the leader anymore
				logger.V(0).Info("Lost leadership", "node", m.nodeIdentifier)
				m.setLeaderStatus(false)
			}

			if initialElection {
				close(m.initialElectionDone)
				initialElection = false
			}
		}
	}
}

// ReleaseLeadership immediately releases leadership if this instance is the leader.
func (m *Manager) ReleaseLeadership(ctx context.Context) error {
	if !m.isLeader {
		return nil // Not a leader, nothing to release
	}

	logger := log.FromContext(ctx)
	logger.V(0).Info("Releasing leadership immediately", "node", m.nodeIdentifier)

	// Get the current lease
	lease, err := m.clientset.CoordinationV1().Leases(m.config.Namespace).Get(ctx, m.config.LockName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Lease doesn't exist, we're not the leader anyway
			m.setLeaderStatus(false)
			return nil
		}
		return fmt.Errorf("failed to get lease for release: %w", err)
	}

	// Check if we're still the holder
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != m.nodeIdentifier {
		// We're not the holder anymore
		m.setLeaderStatus(false)
		return nil
	}

	// Option 1: Delete the lease entirely (fastest handover)
	err = m.clientset.CoordinationV1().Leases(m.config.Namespace).Delete(ctx, m.config.LockName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to delete lease, trying to expire it instead")

		// Option 2: Expire the lease by setting RenewTime to past
		pastTime := metav1.NewMicroTime(time.Now().Add(-time.Duration(*lease.Spec.LeaseDurationSeconds+1) * time.Second))
		lease.Spec.RenewTime = &pastTime
		_, updateErr := m.clientset.CoordinationV1().Leases(m.config.Namespace).Update(ctx, lease, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("failed to expire lease: %w", updateErr)
		}
		logger.V(0).Info("Expired lease to release leadership", "node", m.nodeIdentifier)
	} else {
		logger.V(0).Info("Deleted lease to release leadership", "node", m.nodeIdentifier)
	}

	m.setLeaderStatus(false)
	return nil
}

// StopAndRelease releases leadership and stops the manager.
func (m *Manager) StopAndRelease(ctx context.Context) error {
	if err := m.ReleaseLeadership(ctx); err != nil {
		log.FromContext(ctx).Error(err, "Failed to release leadership during shutdown")
	}

	m.Stop()
	return nil
}

// Stop stops the manager's leader election loop.
func (m *Manager) Stop() {
	close(m.stopCh)
}

// IsLeader returns whether this instance is currently the leader
func (m *Manager) IsLeader() bool {
	return m.isLeader
}

func (m *Manager) setLeaderStatus(isLeader bool) {
	if m.isLeader != isLeader {
		m.isLeader = isLeader
		if m.onLeadershipChange != nil {
			m.onLeadershipChange(isLeader)
		}
	}
}
