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

// Command route-audit replays how the controller places Routes and lists the ones it would reject.
//
//	kubectl get routes.envoyxds.io,listeners.envoyxds.io -n xds-system -o yaml > live.yaml
//	route-audit -nodeID global -cluster global live.yaml new-route.yaml
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	envoyxdsv1alpha1 "github.com/tentens-tech/xds-controller/apis/v1alpha1"
	"github.com/tentens-tech/xds-controller/controllers/rds"
	"github.com/tentens-tech/xds-controller/controllers/util"
	"github.com/tentens-tech/xds-controller/pkg/xds/fcm"
)

func main() {
	os.Exit(run())
}

func run() int {
	file := flag.String("f", "", "Route and Listener manifests; more files can follow as arguments, stdin when none is given")
	nodeID := flag.String("nodeID", "global", "controller --nodeID, used when a route has no nodes annotation")
	cluster := flag.String("cluster", "global", "controller --cluster, used when a route has no clusters annotation")
	flag.Parse()

	files := flag.Args()
	if *file != "" {
		files = append([]string{*file}, files...)
	}
	in, err := readInputs(files)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	report, problems, err := audit(bytes.NewReader(in), *nodeID, *cluster)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for _, line := range report {
		fmt.Println(line)
	}
	if problems > 0 {
		return 1
	}
	return 0
}

// readInputs joins the files as separate YAML documents; "-" or no files reads stdin.
func readInputs(files []string) ([]byte, error) {
	if len(files) == 0 {
		files = []string{"-"}
	}
	var out bytes.Buffer
	for _, f := range files {
		var data []byte
		var err error
		if f == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(f) //nolint:gosec // files the user asked to audit
		}
		if err != nil {
			return nil, err
		}
		out.WriteString("\n---\n")
		out.Write(data)
	}
	return out.Bytes(), nil
}

type placed struct {
	route   *envoyxdsv1alpha1.Route
	matcher *fcm.Matcher
	nodes   []string
}

// audit replays the controller's placement, oldest route first, and reports every
// invalid matcher and every route that would be evicted, ending with a summary line.
func audit(in io.Reader, defaultNode, defaultCluster string) (report []string, problems int, err error) {
	routes, listeners, err := decode(in)
	if err != nil {
		return nil, 0, err
	}
	slices.SortFunc(routes, func(a, b *envoyxdsv1alpha1.Route) int {
		switch {
		case a.CreationTimestamp.IsZero() != b.CreationTimestamp.IsZero():
			// Manifests not applied yet are newer than everything in the cluster.
			if a.CreationTimestamp.IsZero() {
				return 1
			}
			return -1
		case util.OlderRoute(a, b):
			return -1
		case util.OlderRoute(b, a):
			return 1
		}
		return 0
	})

	var served []placed
	for _, r := range routes {
		m, err := fcm.Compile(r.Spec.FilterChainMatch)
		if err != nil {
			report = append(report, fmt.Sprintf("INVALID  %s: %v", r.Name, err))
			continue
		}
		p := placed{route: r, matcher: m, nodes: routeNodes(r, listeners, defaultNode, defaultCluster)}
		if len(p.nodes) == 0 {
			continue
		}
		if line := conflict(p, served, listeners, defaultNode, defaultCluster); line != "" {
			report = append(report, line)
			continue
		}
		served = append(served, p)
	}
	problems = len(report)
	report = append(report, fmt.Sprintf("%d routes, %d problems", len(routes), problems))
	return report, problems, nil
}

func conflict(p placed, served []placed, listeners map[string]*envoyxdsv1alpha1.Listener, defaultNode, defaultCluster string) string {
	for _, name := range p.route.Spec.ListenerRefs {
		l := listeners[name]
		if l == nil {
			continue
		}
		shared := sharedNodes(p.nodes, util.NodeIDs(l.Annotations, defaultNode, defaultCluster))
		if len(shared) == 0 {
			continue
		}
		for _, fc := range l.Spec.FilterChains {
			if fc == nil {
				continue
			}
			if sm, err := fcm.Compile(fc.FilterChainMatch); err == nil && p.matcher.Compare(sm) == fcm.Duplicate {
				return fmt.Sprintf("CONFLICT Listener/%s spec.filter_chains evicts %s (duplicate) on %s", name, p.route.Name, strings.Join(shared, " "))
			}
		}
	}
	for _, s := range served {
		shared := sharedNodes(p.nodes, s.nodes)
		if len(shared) == 0 {
			continue
		}
		if v := rds.RouteConflict(s.route, p.route, s.matcher, p.matcher); v != fcm.None {
			return fmt.Sprintf("CONFLICT %s evicts %s (%s) on %s", s.route.Name, p.route.Name, v, strings.Join(shared, " "))
		}
	}
	return ""
}

// routeNodes mirrors the controller: a route is placed only on nodes where one of its
// listeners exists. Without Listener objects in the input, every listener is assumed present.
func routeNodes(r *envoyxdsv1alpha1.Route, listeners map[string]*envoyxdsv1alpha1.Listener, defaultNode, defaultCluster string) []string {
	nodes := util.NodeIDs(r.Annotations, defaultNode, defaultCluster)
	if len(listeners) == 0 {
		return nodes
	}
	var out []string
	for _, id := range nodes {
		for _, name := range r.Spec.ListenerRefs {
			if l := listeners[name]; l != nil && slices.Contains(util.NodeIDs(l.Annotations, defaultNode, defaultCluster), id) {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

func sharedNodes(a, b []string) []string {
	var out []string
	for _, id := range a {
		if !slices.Contains(b, id) {
			continue
		}
		info, err := util.GetNodeInfo(id)
		if err != nil {
			continue
		}
		out = append(out, strings.Join(info.Clusters, ",")+"/"+strings.Join(info.Nodes, ","))
	}
	return out
}

type object struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Items      []json.RawMessage `json:"items"`
}

// decode reads envoyxds.io Routes and Listeners from multi-document YAML or a List.
func decode(in io.Reader) ([]*envoyxdsv1alpha1.Route, map[string]*envoyxdsv1alpha1.Listener, error) {
	data, err := io.ReadAll(in)
	if err != nil {
		return nil, nil, err
	}
	var routes []*envoyxdsv1alpha1.Route
	listeners := make(map[string]*envoyxdsv1alpha1.Listener)
	add := func(raw []byte) error {
		var head object
		if err := yaml.Unmarshal(raw, &head); err != nil {
			return err
		}
		if head.Kind == "List" || strings.HasSuffix(head.Kind, "List") {
			for _, item := range head.Items {
				if err := addObject(item, &routes, listeners); err != nil {
					return err
				}
			}
			return nil
		}
		return addObject(raw, &routes, listeners)
	}
	for doc := range bytes.SplitSeq(data, []byte("\n---")) {
		if err := add(doc); err != nil {
			return nil, nil, err
		}
	}
	return routes, listeners, nil
}

func addObject(raw []byte, routes *[]*envoyxdsv1alpha1.Route, listeners map[string]*envoyxdsv1alpha1.Listener) error {
	var head object
	if err := yaml.Unmarshal(raw, &head); err != nil {
		return err
	}
	if !strings.HasPrefix(head.APIVersion, envoyxdsv1alpha1.GroupVersion.Group+"/") {
		return nil
	}
	switch head.Kind {
	case "Route":
		r := &envoyxdsv1alpha1.Route{}
		if err := yaml.Unmarshal(raw, r); err != nil {
			return err
		}
		*routes = append(*routes, r)
	case "Listener":
		l := &envoyxdsv1alpha1.Listener{}
		if err := yaml.Unmarshal(raw, l); err != nil {
			return err
		}
		listeners[l.Name] = l
	}
	return nil
}
