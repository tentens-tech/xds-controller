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

package serv

import (
	"context"
	"testing"

	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/status"
	"github.com/tentens-tech/xds-controller/pkg/xds"
)

func rdsListener(t *testing.T, name, route string) *listenerv3.Listener {
	t.Helper()
	hcm, err := anypb.New(&hcmv3.HttpConnectionManager{
		StatPrefix:     name,
		RouteSpecifier: &hcmv3.HttpConnectionManager_Rds{Rds: &hcmv3.Rds{RouteConfigName: route}},
	})
	require.NoError(t, err)
	return &listenerv3.Listener{Name: name, FilterChains: []*listenerv3.FilterChain{{Filters: []*listenerv3.Filter{{
		Name:       wellknown.HTTPConnectionManager,
		ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: hcm},
	}}}}}
}

func TestProcessSnapshot_InconsistentNodeKeepsLastSnapshot(t *testing.T) {
	broken := util.GetNodeID(map[string]string{"clusters": "apps", "nodes": "01"})
	healthy := util.GetNodeID(map[string]string{"clusters": "apps", "nodes": "02"})
	c := cache.NewSnapshotCache(false, cache.IDHash{}, nil)

	previous, err := cache.NewSnapshot("previous", map[resource.Type][]types.Resource{resource.ListenerType: {}})
	require.NoError(t, err)
	require.NoError(t, c.SetSnapshot(context.Background(), broken, previous))

	cfg := &xds.Config{
		ListenerConfigs: map[string][]*listenerv3.Listener{
			broken:  {rdsListener(t, "https", "not-placed-yet")},
			healthy: {rdsListener(t, "https", "placed")},
		},
		RouteConfigs: map[string][]*xds.RouteConfig{
			healthy: {{RouteConfiguration: &routev3.RouteConfiguration{Name: "placed"}}},
		},
		ReconciliationStatus: status.NewReconciliationStatus(),
	}

	err = processSnapshot(context.Background(), &ServerConfig{Cache: c, Config: cfg})
	require.Error(t, err, "the inconsistent node must be retried")

	snap, err := c.GetSnapshot(broken)
	require.NoError(t, err, "the inconsistent node must keep its snapshot")
	assert.Equal(t, "previous", snap.GetVersion(resource.ListenerType))

	snap, err = c.GetSnapshot(healthy)
	require.NoError(t, err, "other nodes must still be updated")
	assert.Len(t, snap.GetResources(resource.RouteType), 1)
}
