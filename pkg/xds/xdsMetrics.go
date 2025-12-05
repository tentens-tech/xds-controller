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

package xds

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type Stream struct {
	Node         string
	Cluster      string
	EnvoyVersion string

	SnapshotClientVersion string
	SnapshotServerVersion string
}

type CurrentServerSnapshot struct {
	Node            string
	Cluster         string
	SnapshotVersion string
}

var (
	Streams   = make(map[int]Stream)
	Snapshots = []CurrentServerSnapshot{}
	mu        sync.RWMutex

	CertExpiryCountDown = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xds_cert_expiry_countdown",
		Help: "Countdown—number of minutes until certificate expiry",
	}, []string{
		"secret_name",
		"domain",
	})

	// Snapshot metrics
	SnapshotVersionMatch = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xds_snapshot_version_match",
		Help: "Indicates if there's a match between current xDS snapshot and Envoy snapshot (1 for match, 0 for mismatch)",
	}, []string{
		"cluster",
		"node",
	})

	SnapshotUpdateCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "xds_snapshot_update_total",
		Help: "Total number of snapshot updates",
	}, []string{
		"cluster",
		"node",
	})

	EnvoyStreamActive = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "xds_envoy_stream_active",
		Help: "Active Envoy stream information",
	}, []string{
		"id",
		"cluster",
		"node",
		"version",
	})

	// Resource metrics
	ResourceCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xds_resource_count",
		Help: "Number of resources by type, cluster, and node",
	}, []string{
		"resource_type",
		"cluster",
		"node",
	})

	// Error metrics
	ErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "xds_error_total",
		Help: "Total number of errors encountered",
	}, []string{
		"resource_type",
	})

	// Configuration metrics
	ConfigErrorCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xds_config_error_count",
		Help: "Number of configurations with errors that were not applied to the snapshot",
	}, []string{
		"name",
		"resource_type",
		"error",
	})
)

func init() {
	metrics.Registry.MustRegister(
		CertExpiryCountDown,
		SnapshotVersionMatch,
		SnapshotUpdateCounter,
		EnvoyStreamActive,
		ConfigErrorCount,
		ResourceCount,
		ErrorCounter,
	)
}

// RecordEnvoyStreamActive records the active Envoy stream for a given cluster and node.
func RecordEnvoyStreamActive(id, cluster, node, version string) {
	EnvoyStreamActive.With(prometheus.Labels{
		"id":      id,
		"cluster": cluster,
		"node":    node,
		"version": version,
	})
}

// RecordSnapshotVersionMatchSet records whether the snapshot version matches between Envoy and xDS.
func RecordSnapshotVersionMatchSet(cluster, node string) {
	if VersionMatch(cluster, node) {
		SnapshotVersionMatch.With(prometheus.Labels{
			"cluster": cluster,
			"node":    node,
		}).Set(1)
	} else {
		SnapshotVersionMatch.With(prometheus.Labels{
			"cluster": cluster,
			"node":    node,
		}).Set(0)
	}
}

// RecordCertificateMetrics records the expiry time of a certificate and the number of minutes until it expires.
func RecordCertificateMetrics(secretName, domain string, expiryMinutes float64) {
	CertExpiryCountDown.With(prometheus.Labels{
		"secret_name": secretName,
		"domain":      domain,
	}).Set(expiryMinutes)
}

// RecordSnapshotUpdateCount records the number of updates to the snapshot for a given cluster and node.
func RecordSnapshotUpdateCount(cluster, node string) {
	SnapshotUpdateCounter.With(prometheus.Labels{
		"cluster": cluster,
		"node":    node,
	}).Inc()
}

// RecordResourceCount records the number of resources of a given type, cluster, and node.
func RecordResourceCount(resourceType, cluster, node string, count float64) {
	ResourceCount.With(prometheus.Labels{
		"resource_type": resourceType,
		"cluster":       cluster,
		"node":          node,
	}).Set(count)
}

// RecordError records an error in the xDS controller.
func RecordError(resourceType string) {
	ErrorCounter.With(prometheus.Labels{
		"resource_type": resourceType,
	}).Inc()
}

// RecordConfigError records an error in the configuration of a resource.
func RecordConfigError(name, resourceType, err string) {
	ConfigErrorCount.With(prometheus.Labels{
		"name":          name,
		"resource_type": resourceType,
		"error":         err,
	}).Inc()

	RecordError(resourceType)
}

// SetSnapshotClientVersion updates the SnapshotClientVersion for all streams matching the Node and Cluster
func SetSnapshotClientVersion(node, cluster, version string) {
	mu.Lock()
	defer mu.Unlock()
	for id, stream := range Streams {
		if stream.Node == node && stream.Cluster == cluster {
			stream.SnapshotClientVersion = version
			Streams[id] = stream
		}
	}
}

// SetSnapshotServerVersion updates the SnapshotServerVersion for all streams matching the Node and Cluster
func SetSnapshotServerVersion(node, cluster, version string) {
	mu.Lock()
	defer mu.Unlock()
	for id, stream := range Streams {
		if stream.Node == node && stream.Cluster == cluster {
			stream.SnapshotServerVersion = version
			Streams[id] = stream
		}
	}
}

// VersionMatch checks if SnapshotClientVersion and SnapshotServerVersion match for a given cluster and node
func VersionMatch(cluster, node string) bool {
	mu.RLock()
	defer mu.RUnlock()

	for _, stream := range Streams {
		if stream.Cluster == cluster && stream.Node == node {
			if stream.SnapshotClientVersion != "" && stream.SnapshotServerVersion != "" {
				return stream.SnapshotClientVersion == stream.SnapshotServerVersion
			}
			// If either version is empty, we consider it a non-match
			return false
		}
	}

	// If no matching stream is found, we consider it a non-match
	return false
}

func CleanSnapshots() {
	mu.Lock()
	defer mu.Unlock()
	Snapshots = []CurrentServerSnapshot{}
}

func AddSnapshot(node, cluster, version string) {
	mu.Lock()
	defer mu.Unlock()

	for i := range Snapshots {
		if Snapshots[i].Node == node && Snapshots[i].Cluster == cluster {
			Snapshots[i].SnapshotVersion = version
			return
		}
	}

	s := CurrentServerSnapshot{
		Node:            node,
		Cluster:         cluster,
		SnapshotVersion: version,
	}

	Snapshots = append(Snapshots, s)
}

func GetSnapshots() []CurrentServerSnapshot {
	return Snapshots
}
