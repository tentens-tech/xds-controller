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

// Package util provides utility functions for the xDS controllers.
package util

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
)

// NodeInfo contains node and cluster information parsed from annotations.
type NodeInfo struct {
	Nodes    []string `json:"nodes"`
	Clusters []string `json:"clusters"`
}

// GetNodeID generates a base64-encoded node ID from annotations.
func GetNodeID(an map[string]string) string {
	// get nodes and clusters from annotations which divided by comma and put it into a NodeInfo json
	nodeInfo := NodeInfo{}
	if an["nodes"] != "" {
		// split nodes by comma and sort before put it into NodeInfo json
		nodes := strings.Split(an["nodes"], ",")
		sort.Strings(nodes)
		nodeInfo.Nodes = nodes
	}
	if an["clusters"] != "" {
		// split clusters by comma and sort before put it into NodeInfo json
		clusters := strings.Split(an["clusters"], ",")
		sort.Strings(clusters)
		nodeInfo.Clusters = clusters
	}

	// marshal NodeInfo json - error is ignored as NodeInfo only contains []string which cannot fail
	nodeInfoData, _ := json.Marshal(nodeInfo) //nolint:errcheck // json.Marshal cannot fail for []string fields

	// get base64 string from NodeInfo json
	return base64.StdEncoding.EncodeToString(nodeInfoData)
}

// GetNodeInfo decodes a base64-encoded node ID and returns NodeInfo.
func GetNodeInfo(nodeid string) (NodeInfo, error) {
	// decode base64 string to NodeInfo json
	nodeInfoData, err := base64.StdEncoding.DecodeString(nodeid)
	if err != nil {
		return NodeInfo{}, err
	}

	// unmarshal NodeInfo json
	nodeInfo := NodeInfo{}
	err = json.Unmarshal(nodeInfoData, &nodeInfo)
	if err != nil {
		return NodeInfo{}, err
	}

	return nodeInfo, nil
}

// FindNodeAndCluster checks if there's at least one matching node and cluster between
// the provided NodeInfo and the base64 encoded nodeId
func (n NodeInfo) FindNodeAndCluster(nodeid string) bool {
	compareInfo, err := GetNodeInfo(nodeid)
	if err != nil {
		return false
	}

	// Check if any node matches
	nodeMatched := false
	for _, node1 := range n.Nodes {
		for _, node2 := range compareInfo.Nodes {
			if node1 == node2 {
				nodeMatched = true
				break
			}
		}
		if nodeMatched {
			break
		}
	}

	if !nodeMatched {
		return false
	}

	// Check if any cluster matches
	for _, cluster1 := range n.Clusters {
		for _, cluster2 := range compareInfo.Clusters {
			if cluster1 == cluster2 {
				return true
			}
		}
	}

	return false
}

// ParseCSV splits a comma-separated string into trimmed parts.
func ParseCSV(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
