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

package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
)

func TestNodeIDs(t *testing.T) {
	ids := NodeIDs(map[string]string{"clusters": "apps, edge", "nodes": "02,01"}, "global", "global")
	pairs := make([]string, 0, len(ids))
	for _, id := range ids {
		info, err := GetNodeInfo(id)
		require.NoError(t, err)
		pairs = append(pairs, info.Clusters[0]+"/"+info.Nodes[0])
	}
	assert.Equal(t, []string{"apps/01", "apps/02", "edge/01", "edge/02"}, pairs)

	ids = NodeIDs(nil, "n", "c")
	require.Len(t, ids, 1)
	assert.Equal(t, GetNodeID(map[string]string{"clusters": "c", "nodes": "n"}), ids[0])
}

func TestOlderRoute(t *testing.T) {
	route := func(name string, created time.Time) *envoyxdsv1alpha1.Route {
		return &envoyxdsv1alpha1.Route{ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(created)}}
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	assert.True(t, OlderRoute(route("z", t0), route("a", t0.Add(time.Second))))
	assert.False(t, OlderRoute(route("a", t0.Add(time.Second)), route("z", t0)))
	assert.True(t, OlderRoute(route("a", t0), route("b", t0)))
	assert.False(t, OlderRoute(route("b", t0), route("a", t0)))
	assert.False(t, OlderRoute(route("a", t0), route("a", t0)))
}
