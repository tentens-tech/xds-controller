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
)

// rdsListener returns a listener whose single filter chain uses the named route configuration.
func rdsListener(t testing.TB, name, route string) *listenerv3.Listener {
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

func TestPruneUnreferencedRoutes(t *testing.T) {
	res := map[string][]types.Resource{
		resource.ListenerType: {rdsListener(t, "https", "used")},
		resource.RouteType:    {&routev3.RouteConfiguration{Name: "used"}, &routev3.RouteConfiguration{Name: "skipped-by-lds"}},
	}
	pruneUnreferencedRoutes(res, routeReferences(res[resource.ListenerType], nil))

	require.Len(t, res[resource.RouteType], 1)
	assert.Equal(t, "used", cache.GetResourceName(res[resource.RouteType][0]))

	noListeners := map[string][]types.Resource{resource.RouteType: {&routev3.RouteConfiguration{Name: "orphan"}}}
	pruneUnreferencedRoutes(noListeners, routeReferences(nil, nil))
	assert.Empty(t, noListeners[resource.RouteType])
}
