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

// Package main generates HCM (HTTP Connection Manager) types from go-control-plane.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	// Import all common Envoy packages to build the type registry
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	envoytype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

// buildTypeRegistry creates a map of type names to their reflect.Type
func buildTypeRegistry() map[string]reflect.Type {
	registry := make(map[string]reflect.Type)

	typesToScan := []interface{}{
		// Core package types
		core.DataSource{},
		core.Metadata{},
		core.HeaderValue{},
		core.HeaderValueOption{},
		core.RuntimeFractionalPercent{},
		core.TypedExtensionConfig{},
		core.ConfigSource{},
		core.SubstitutionFormatString{},
		core.PathConfigSource{},
		core.ApiConfigSource{},
		core.AggregatedConfigSource{},
		core.SelfConfigSource{},
		core.RateLimitSettings{},
		core.GrpcService{},
		core.GrpcService_EnvoyGrpc{},
		core.GrpcService_GoogleGrpc{},
		core.GrpcService_GoogleGrpc_CallCredentials{},
		core.GrpcService_GoogleGrpc_ChannelCredentials{},
		core.Address{},
		core.SocketAddress{},
		core.Pipe{},
		core.EnvoyInternalAddress{},
		core.CidrRange{},
		core.RetryPolicy{},
		core.Http1ProtocolOptions{},
		core.Http2ProtocolOptions{},
		core.Http3ProtocolOptions{},
		core.HttpProtocolOptions{},
		core.SchemeHeaderTransformation{},
		core.ProxyProtocolConfig{},
		core.HttpUri{},
		core.ExtensionConfigSource{},
		// HCM package types
		hcm.HttpConnectionManager{},
		hcm.HttpConnectionManager_Tracing{},
		hcm.HttpConnectionManager_InternalAddressConfig{},
		hcm.HttpConnectionManager_SetCurrentClientCertDetails{},
		hcm.HttpConnectionManager_UpgradeConfig{},
		hcm.HttpConnectionManager_PathNormalizationOptions{},
		hcm.HttpConnectionManager_ProxyStatusConfig{},
		hcm.HttpConnectionManager_HcmAccessLogOptions{},
		hcm.LocalReplyConfig{},
		hcm.ResponseMapper{},
		hcm.Rds{},
		hcm.ScopedRoutes{},
		hcm.ScopedRoutes_ScopeKeyBuilder{},
		hcm.ScopedRoutes_ScopeKeyBuilder_FragmentBuilder{},
		hcm.ScopedRoutes_ScopeKeyBuilder_FragmentBuilder_HeaderValueExtractor{},
		hcm.ScopedRds{},
		hcm.HttpFilter{},
		hcm.RequestIDExtension{},
		hcm.ScopedRouteConfigurationsList{},
		// Route package types (for ScopedRouteConfiguration and route actions)
		route.ScopedRouteConfiguration{},
		route.RouteConfiguration{},
		route.VirtualHost{},
		route.Route{},
		route.RouteMatch{},
		route.RouteAction{},
		route.RedirectAction{},
		route.DirectResponseAction{},
		route.FilterAction{},
		route.NonForwardingAction{},
		route.WeightedCluster{},
		route.WeightedCluster_ClusterWeight{},
		route.RetryPolicy{},
		route.HedgePolicy{},
		route.CorsPolicy{},
		route.Tracing{},
		route.HeaderMatcher{},
		route.QueryParameterMatcher{},
		route.InternalRedirectPolicy{},
		route.Decorator{},
		// Access log types
		accesslog.AccessLog{},
		accesslog.AccessLogFilter{},
		accesslog.ComparisonFilter{},
		accesslog.StatusCodeFilter{},
		accesslog.DurationFilter{},
		accesslog.NotHealthCheckFilter{},
		accesslog.TraceableFilter{},
		accesslog.RuntimeFilter{},
		accesslog.AndFilter{},
		accesslog.OrFilter{},
		accesslog.HeaderFilter{},
		accesslog.ResponseFlagFilter{},
		accesslog.GrpcStatusFilter{},
		accesslog.MetadataFilter{},
		accesslog.LogTypeFilter{},
		accesslog.ExtensionFilter{},
		// Type package
		envoytype.FractionalPercent{},
		envoytype.Percent{},
		envoytype.Int64Range{},
		envoytype.DoubleRange{},
		// Matcher types
		matcher.RegexMatcher{},
		matcher.StringMatcher{},
	}

	for _, t := range typesToScan {
		rt := reflect.TypeOf(t)
		if rt.Kind() == reflect.Ptr {
			rt = rt.Elem()
		}
		if rt.Kind() == reflect.Struct {
			registry[rt.Name()] = rt
		}
	}

	return registry
}

var (
	outputDir = flag.String("output", "pkg/xds/types/hcm", "Output directory")
	verbose   = flag.Bool("v", false, "Verbose output")
	envoyTag  = flag.String("envoy-tag", "v1.36.0", "Envoy module tag for fetching source")
)

const githubRawBase = "https://raw.githubusercontent.com/envoyproxy/go-control-plane"

func main() {
	flag.Parse()

	gen := &Generator{
		outputDir:      *outputDir,
		verbose:        *verbose,
		envoyTag:       *envoyTag,
		processedTypes: make(map[string]bool),
		imports:        make(map[string]string),
		enumDefs:       make(map[string]*EnumDef),
		typeDefs:       make(map[string]*TypeDef),
		oneofWrappers:  make(map[string][]OneofOption),
		typeRegistry:   buildTypeRegistry(),
		skipFields:     make(map[string]map[string]bool),
	}

	// Skip route_config field in HCM since we use our own RDS type
	gen.skipFields["HCM"] = map[string]bool{
		"RouteConfig": true, // Will be provided by route.Route type
	}

	if err := gen.Generate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated HCM types in %s\n", *outputDir)
}

type EnumDef struct {
	Name   string
	Values []EnumValue
}

type EnumValue struct {
	Name  string
	Value int32
}

type TypeDef struct {
	Name   string
	Fields []FieldDef
}

type FieldDef struct {
	Name     string
	Type     string
	JSONName string
}

type OneofOption struct {
	FieldName string
	FieldType string
	JSONName  string
}

type Generator struct {
	outputDir      string
	verbose        bool
	envoyTag       string
	processedTypes map[string]bool
	imports        map[string]string
	enumDefs       map[string]*EnumDef
	typeDefs       map[string]*TypeDef
	typeQueue      []queuedType
	oneofWrappers  map[string][]OneofOption
	typeRegistry   map[string]reflect.Type
	skipFields     map[string]map[string]bool
}

type queuedType struct {
	t          reflect.Type
	outputName string
}

func (g *Generator) Generate() error {
	if err := os.MkdirAll(g.outputDir, 0o750); err != nil { // #nosec G301 -- tool output directory
		return fmt.Errorf("creating output directory: %w", err)
	}

	fmt.Println("Fetching enum definitions from GitHub...")
	g.fetchEnumDefinitions()

	// Start with the HttpConnectionManager type
	hcmType := reflect.TypeOf(hcm.HttpConnectionManager{})
	g.queueType(hcmType, "HCM")

	// Process all queued types
	for len(g.typeQueue) > 0 {
		item := g.typeQueue[0]
		g.typeQueue = g.typeQueue[1:]
		g.processType(item.t, item.outputName)
	}

	// Post-process: replace empty types with RawExtension
	g.replaceEmptyTypes()

	// Post-process: replace recursive types with RawExtension
	g.replaceRecursiveTypes()

	return g.writeOutput()
}

func (g *Generator) replaceEmptyTypes() {
	emptyTypes := make(map[string]bool)
	for name, typeDef := range g.typeDefs {
		if len(typeDef.Fields) == 0 {
			emptyTypes[name] = true
			if g.verbose {
				fmt.Printf("Found empty type: %s - will use RawExtension\n", name)
			}
		}
	}

	if len(emptyTypes) == 0 {
		return
	}

	g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"

	for _, typeDef := range g.typeDefs {
		for i, field := range typeDef.Fields {
			newType := g.replaceEmptyTypeRef(field.Type, emptyTypes)
			if newType != field.Type {
				typeDef.Fields[i].Type = newType
				if g.verbose {
					fmt.Printf("  Replaced %s.%s type: %s -> %s\n", typeDef.Name, field.Name, field.Type, newType)
				}
			}
		}
	}

	for name := range emptyTypes {
		delete(g.typeDefs, name)
	}
}

func (g *Generator) replaceEmptyTypeRef(fieldType string, emptyTypes map[string]bool) string {
	if strings.HasPrefix(fieldType, "*") {
		inner := strings.TrimPrefix(fieldType, "*")
		if emptyTypes[inner] {
			return "*runtime.RawExtension"
		}
	}
	if strings.HasPrefix(fieldType, "[]*") {
		inner := strings.TrimPrefix(fieldType, "[]*")
		if emptyTypes[inner] {
			return "[]*runtime.RawExtension"
		}
	}
	if strings.HasPrefix(fieldType, "[]") {
		inner := strings.TrimPrefix(fieldType, "[]")
		if emptyTypes[inner] {
			return "[]*runtime.RawExtension"
		}
	}
	return fieldType
}

func (g *Generator) replaceRecursiveTypes() {
	g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"

	for typeName, typeDef := range g.typeDefs {
		for i, field := range typeDef.Fields {
			if g.isRecursiveField(typeName, field.Type) {
				var newType string
				if strings.HasPrefix(field.Type, "[]*") {
					newType = "[]*runtime.RawExtension"
				} else {
					newType = "*runtime.RawExtension"
				}
				if g.verbose {
					fmt.Printf("Replacing recursive field %s.%s: %s -> %s\n", typeName, field.Name, field.Type, newType)
				}
				typeDef.Fields[i].Type = newType
			}
		}
	}
}

func (g *Generator) fetchEnumDefinitions() {
	fetchedPkgs := make(map[string]bool)

	// Discover packages from all types in the registry
	for _, rt := range g.typeRegistry {
		g.discoverPackages(rt, fetchedPkgs)
	}

	// Also add HCM and related packages explicitly
	fetchedPkgs["envoy/extensions/filters/network/http_connection_manager/v3"] = true
	fetchedPkgs["envoy/config/core/v3"] = true
	fetchedPkgs["envoy/config/accesslog/v3"] = true
	fetchedPkgs["envoy/type/v3"] = true

	if g.verbose {
		fmt.Printf("Discovered %d packages to fetch\n", len(fetchedPkgs))
	}

	for pkg := range fetchedPkgs {
		g.fetchPackageEnums(pkg)
	}

	if g.verbose {
		fmt.Printf("Found %d enum definitions\n", len(g.enumDefs))
	}
}

func (g *Generator) discoverPackages(t reflect.Type, seen map[string]bool) {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return
	}

	pkgPath := t.PkgPath()
	if !strings.Contains(pkgPath, "envoyproxy") {
		return
	}

	if idx := strings.Index(pkgPath, "envoy/"); idx != -1 {
		subPath := pkgPath[idx:]
		if !seen[subPath] {
			seen[subPath] = true
			for i := 0; i < t.NumField(); i++ {
				g.discoverPackages(t.Field(i).Type, seen)
			}
		}
	}
}

func (g *Generator) fetchPackageEnums(pkgPath string) {
	parts := strings.Split(pkgPath, "/")
	if len(parts) < 3 {
		return
	}

	baseURL := fmt.Sprintf("%s/envoy/%s/%s", githubRawBase, g.envoyTag, pkgPath)
	dirName := parts[len(parts)-2]

	filesToTry := []string{
		dirName + ".pb.go",
		dirName + "_components.pb.go",
		"http_connection_manager.pb.go",
		"base.pb.go",
		"config_source.pb.go",
		"protocol.pb.go",
		"address.pb.go",
		"grpc_service.pb.go",
		"accesslog.pb.go",
		"percent.pb.go",
	}

	for _, file := range filesToTry {
		fileURL := fmt.Sprintf("%s/%s", baseURL, file)
		if g.verbose {
			fmt.Printf("Fetching %s\n", fileURL)
		}
		_ = g.fetchAndParseEnums(fileURL) //nolint:errcheck // Ignore error - some files may not exist
	}
}

func (g *Generator) fetchAndParseEnums(fileURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, http.NoBody)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req) // #nosec G107 -- URL is constructed from trusted package paths
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // closing body in defer

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return g.parseEnumsFromReader(resp.Body)
}

// pendingOneofField holds a discovered oneof field that needs to be matched to its interface later
type pendingOneofField struct {
	wrapperKey string // e.g., "HttpConnectionManager_Rds"
	fieldName  string
	fieldType  string
	jsonName   string
}

func (g *Generator) parseEnumsFromReader(r io.Reader) error {
	// Read all content first so we can do a two-pass approach
	content, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	enumNamePattern := regexp.MustCompile(`^\s*(\w+)_name\s*=\s*map\[int32\]string\s*\{`)
	enumValuePattern := regexp.MustCompile(`^\s*(\d+):\s*"(\w+)"`)
	oneofInterfacePattern := regexp.MustCompile(`^type\s+(is\w+)\s+interface\s*\{`)
	oneofWrapperPattern := regexp.MustCompile(`^type\s+(\w+)_(\w+)\s+struct\s*\{`)
	oneofFieldPattern := regexp.MustCompile(`^\s*(\w+)\s+(\S+)\s+` + "`" + `protobuf:"[^"]*name=([^,"]+)[^"]*,oneof"`)
	// Pattern to match interface implementation: func (*ParentType_FieldName) isParentType_InterfaceName() {}
	oneofImplPattern := regexp.MustCompile(`^func\s+\(\*(\w+)_(\w+)\)\s+(is\w+)\(\)\s*\{\}`)

	oneofInterfaces := make(map[string]bool)
	// Map from "ParentType_FieldName" to its interface name
	wrapperToInterface := make(map[string]string)
	// Pending oneof fields to be matched after we've collected all wrapper-to-interface mappings
	var pendingFields []pendingOneofField

	// First pass: collect interfaces and wrapper-to-interface mappings
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := scanner.Text()

		if matches := oneofInterfacePattern.FindStringSubmatch(line); matches != nil {
			oneofInterfaces[matches[1]] = true
			continue
		}

		// Parse interface implementation methods
		if matches := oneofImplPattern.FindStringSubmatch(line); matches != nil {
			parentType := matches[1]
			fieldName := matches[2]
			interfaceName := matches[3]
			wrapperKey := parentType + "_" + fieldName
			wrapperToInterface[wrapperKey] = interfaceName
			if g.verbose {
				fmt.Printf("  Mapped wrapper %s to interface %s\n", wrapperKey, interfaceName)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Second pass: collect enums and oneof fields
	scanner = bufio.NewScanner(strings.NewReader(string(content)))
	var currentEnum string
	var currentParentType string
	var currentWrapperFieldName string
	var inEnumBlock bool
	var inOneofWrapper bool

	for scanner.Scan() {
		line := scanner.Text()

		if matches := oneofWrapperPattern.FindStringSubmatch(line); matches != nil {
			currentParentType = matches[1]
			currentWrapperFieldName = matches[2]
			inOneofWrapper = true
			continue
		}

		if inOneofWrapper {
			// Check for struct closing brace - must be just "}" with optional whitespace
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine == "}" {
				inOneofWrapper = false
				currentParentType = ""
				currentWrapperFieldName = ""
				continue
			}

			if matches := oneofFieldPattern.FindStringSubmatch(line); matches != nil {
				fieldName := matches[1]
				fieldType := matches[2]
				jsonName := matches[3]

				wrapperKey := currentParentType + "_" + currentWrapperFieldName
				pendingFields = append(pendingFields, pendingOneofField{
					wrapperKey: wrapperKey,
					fieldName:  fieldName,
					fieldType:  fieldType,
					jsonName:   jsonName,
				})
			}
		}

		if matches := enumNamePattern.FindStringSubmatch(line); matches != nil {
			currentEnum = matches[1]
			inEnumBlock = true
			if g.enumDefs[currentEnum] == nil {
				g.enumDefs[currentEnum] = &EnumDef{Name: currentEnum}
			}
			continue
		}

		if inEnumBlock {
			if strings.Contains(line, "}") {
				inEnumBlock = false
				currentEnum = ""
				continue
			}

			if matches := enumValuePattern.FindStringSubmatch(line); matches != nil {
				var value int32
				if _, err := fmt.Sscanf(matches[1], "%d", &value); err != nil {
					continue
				}
				alreadyExists := false
				for _, v := range g.enumDefs[currentEnum].Values {
					if v.Value == value {
						alreadyExists = true
						break
					}
				}
				if !alreadyExists {
					g.enumDefs[currentEnum].Values = append(g.enumDefs[currentEnum].Values, EnumValue{
						Name:  matches[2],
						Value: value,
					})
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Now match pending fields to their interfaces
	for _, pf := range pendingFields {
		ifaceName, hasExactMatch := wrapperToInterface[pf.wrapperKey]

		if !hasExactMatch {
			// Fall back to finding any matching interface (for backwards compatibility)
			parts := strings.SplitN(pf.wrapperKey, "_", 2)
			if len(parts) < 2 {
				continue
			}
			parentType := parts[0]
			for iface := range oneofInterfaces {
				if strings.HasPrefix(iface, "is"+parentType+"_") {
					ifaceName = iface
					break
				}
			}
		}

		if ifaceName == "" {
			continue
		}

		alreadyExists := false
		for _, existing := range g.oneofWrappers[ifaceName] {
			if existing.FieldName == pf.fieldName {
				alreadyExists = true
				break
			}
		}
		if alreadyExists {
			continue
		}

		option := OneofOption{
			FieldName: pf.fieldName,
			FieldType: pf.fieldType,
			JSONName:  pf.jsonName,
		}
		g.oneofWrappers[ifaceName] = append(g.oneofWrappers[ifaceName], option)
		if g.verbose {
			fmt.Printf("  Discovered oneof: %s -> %s %s `json:\"%s\"`\n", ifaceName, pf.fieldName, pf.fieldType, pf.jsonName)
		}

		// Dynamic: queue the discovered type
		g.queueDiscoveredType(pf.fieldType)
	}

	return nil
}

func (g *Generator) queueDiscoveredType(fieldType string) {
	typeName := strings.TrimPrefix(fieldType, "*")
	if idx := strings.LastIndex(typeName, "."); idx >= 0 {
		typeName = typeName[idx+1:]
	}

	if rt, ok := g.typeRegistry[typeName]; ok {
		if !g.processedTypes[typeName] {
			if g.verbose {
				fmt.Printf("  Auto-queueing discovered type: %s\n", typeName)
			}
			g.queueType(rt, "")
		}
	}
}

func (g *Generator) queueType(t reflect.Type, outputName string) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return
	}

	key := outputName
	if key == "" {
		key = t.Name()
	}

	if g.processedTypes[key] {
		return
	}
	g.processedTypes[key] = true

	g.typeQueue = append(g.typeQueue, queuedType{t: t, outputName: outputName})
}

func (g *Generator) processType(t reflect.Type, outputName string) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return
	}

	typeName := outputName
	if typeName == "" {
		typeName = t.Name()
	}

	if strings.HasPrefix(typeName, "is") || typeName == "" {
		return
	}

	if g.verbose {
		fmt.Printf("Processing type: %s\n", typeName)
	}

	typeDef := &TypeDef{Name: typeName}

	hasTypedConfigField := false
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Name == "TypedConfig" {
			hasTypedConfigField = true
			break
		}
	}

	if g.typesNeedingTypedConfig(typeName) && !hasTypedConfigField {
		g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"
		typeDef.Fields = append(typeDef.Fields, FieldDef{
			Name:     "TypedConfig",
			Type:     "*runtime.RawExtension",
			JSONName: "typed_config",
		})
		if g.verbose {
			fmt.Printf("  Added TypedConfig for type %s\n", typeName)
		}
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if !field.IsExported() ||
			field.Name == "state" ||
			field.Name == "sizeCache" ||
			field.Name == "unknownFields" ||
			strings.HasPrefix(field.Name, "XXX_") {
			continue
		}

		// Check if this field should be skipped for this type
		if skipFields, ok := g.skipFields[typeName]; ok {
			if skipFields[field.Name] {
				if g.verbose {
					fmt.Printf("  Skipping field %s in type %s (configured skip)\n", field.Name, typeName)
				}
				continue
			}
		}

		if field.Type.Kind() == reflect.Interface {
			interfaceTypeName := field.Type.Name()

			if options, ok := g.oneofWrappers[interfaceTypeName]; ok && len(options) > 0 {
				if g.verbose {
					fmt.Printf("  Expanding oneof %s with %d options (dynamic)\n", interfaceTypeName, len(options))
				}
				for _, opt := range options {
					fieldExists := false
					for _, f := range typeDef.Fields {
						if f.Name == opt.FieldName {
							fieldExists = true
							break
						}
					}
					if fieldExists {
						continue
					}

					fieldType := g.simplifyOneofType(opt.FieldType)

					if fieldType != "" {
						typeDef.Fields = append(typeDef.Fields, FieldDef{
							Name:     opt.FieldName,
							Type:     fieldType,
							JSONName: opt.JSONName,
						})
						if g.verbose {
							fmt.Printf("    Added oneof option: %s %s `json:\"%s\"`\n", opt.FieldName, fieldType, opt.JSONName)
						}
					}
				}
			} else if g.isConfigTypeOneof(field.Name) {
				g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"
				hasTypedConfig := false
				for _, f := range typeDef.Fields {
					if f.Name == "TypedConfig" {
						hasTypedConfig = true
						break
					}
				}
				if !hasTypedConfig {
					typeDef.Fields = append(typeDef.Fields, FieldDef{
						Name:     "TypedConfig",
						Type:     "*runtime.RawExtension",
						JSONName: "typed_config",
					})
					if g.verbose {
						fmt.Printf("  Added TypedConfig for config oneof field %s\n", field.Name)
					}
				}
			} else if g.verbose {
				fmt.Printf("  Skipping unknown oneof interface: %s (type %s)\n", field.Name, interfaceTypeName)
			}
			continue
		}

		fieldType := g.mapFieldType(field.Type)
		if fieldType == "" {
			continue
		}

		if fieldType == "*"+typeName || fieldType == typeName {
			if g.verbose {
				fmt.Printf("  Skipping self-referential field %s in type %s\n", field.Name, typeName)
			}
			continue
		}

		jsonName := g.getJSONName(field)
		if jsonName == "-" {
			continue
		}

		typeDef.Fields = append(typeDef.Fields, FieldDef{
			Name:     field.Name,
			Type:     fieldType,
			JSONName: jsonName,
		})
	}

	g.typeDefs[typeName] = typeDef
}

func (g *Generator) isConfigTypeOneof(fieldName string) bool {
	configOneofs := []string{
		"ConfigType",
		"RouteSpecifier",
		"StripPortMode",
		"Specifier",
		"MatchPattern",
		"ActionSpecifier",
	}
	for _, name := range configOneofs {
		if fieldName == name {
			return true
		}
	}
	return false
}

func (g *Generator) simplifyOneofType(fieldType string) string {
	isPointer := strings.HasPrefix(fieldType, "*")
	if isPointer {
		fieldType = strings.TrimPrefix(fieldType, "*")
	}

	if idx := strings.LastIndex(fieldType, "."); idx >= 0 {
		fieldType = fieldType[idx+1:]
	}

	switch fieldType {
	case "bool", "string", "int32", "int64", "uint32", "uint64", "float32", "float64":
		return fieldType
	case "Any":
		g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"
		return "*runtime.RawExtension"
	case "BoolValue":
		return "*bool"
	case "UInt32Value":
		return "*uint32"
	case "UInt64Value":
		return "*uint64"
	case "Int32Value":
		return "*int32"
	case "Int64Value":
		return "*int64"
	case "FloatValue":
		return "*float32"
	case "DoubleValue":
		return "*float64"
	case "StringValue":
		return "*string"
	case "Duration":
		return "*string"
	case "Struct":
		g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"
		return "*runtime.RawExtension"
	case "Empty":
		// protobuf Empty type - use bool to indicate presence
		return "*bool"
	// HCM-specific oneof types
	case "Rds":
		return "*Rds"
	case "ScopedRoutes":
		return "*ScopedRoutes"
	case "RouteConfiguration":
		// This is the inline route config - use RawExtension since it's complex
		g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"
		return "*runtime.RawExtension"
	}

	if strings.HasPrefix(fieldType, "[]") {
		return fieldType
	}

	if _, isEnum := g.enumDefs[fieldType]; isEnum {
		return fieldType
	}

	if strings.Contains(fieldType, "_") && isPointer {
		parts := strings.Split(fieldType, "_")
		if len(parts) >= 2 {
			g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"
			return "*runtime.RawExtension"
		}
	}

	if !isPointer && !g.processedTypes[fieldType] {
		return ""
	}

	if isPointer {
		return "*" + fieldType
	}
	return fieldType
}

func (g *Generator) typesNeedingTypedConfig(typeName string) bool {
	needsTypedConfig := []string{
		"HttpFilter",
		"TypedExtensionConfig",
		"ExtensionFilter",
		"AccessLog",
		"RequestIDExtension",
	}
	for _, name := range needsTypedConfig {
		if typeName == name {
			return true
		}
	}
	return false
}

func (g *Generator) mapFieldType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Ptr:
		inner := g.mapFieldType(t.Elem())
		if inner == "" {
			return ""
		}
		if strings.HasPrefix(inner, "*") {
			return inner
		}
		return "*" + inner

	case reflect.Slice:
		elem := g.mapFieldType(t.Elem())
		if elem == "" {
			return ""
		}
		return "[]" + elem

	case reflect.Map:
		key := g.mapFieldType(t.Key())
		val := g.mapFieldType(t.Elem())
		if key == "" || val == "" {
			return ""
		}
		return fmt.Sprintf("map[%s]%s", key, val)

	case reflect.Struct:
		return g.mapStructType(t)

	case reflect.Interface:
		return ""

	case reflect.Bool:
		return "bool"
	case reflect.Int32:
		if t.Name() != "" && t.Name() != "int32" {
			return g.mapEnumType(t)
		}
		return "int32"
	case reflect.Int64:
		return "int64"
	case reflect.Uint32:
		return "uint32"
	case reflect.Uint64:
		return "uint64"
	case reflect.Float32:
		return "float32"
	case reflect.Float64:
		return "float64"
	case reflect.String:
		return "string"
	case reflect.Uint8:
		return "byte"
	default:
		return ""
	}
}

func (g *Generator) mapStructType(t reflect.Type) string {
	pkgPath := t.PkgPath()
	typeName := t.Name()
	fullName := pkgPath + "." + typeName

	switch {
	case strings.Contains(fullName, "durationpb.Duration"):
		return "string"
	case strings.Contains(fullName, "anypb.Any"):
		g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"
		return "*runtime.RawExtension"
	case strings.Contains(fullName, "structpb.Struct"):
		g.imports["k8s.io/apimachinery/pkg/runtime"] = "runtime"
		return "*runtime.RawExtension"
	case strings.Contains(fullName, "wrapperspb.BoolValue"):
		return "*bool"
	case strings.Contains(fullName, "wrapperspb.UInt32Value"):
		return "*uint32"
	case strings.Contains(fullName, "wrapperspb.UInt64Value"):
		return "*uint64"
	case strings.Contains(fullName, "wrapperspb.Int32Value"):
		return "*int32"
	case strings.Contains(fullName, "wrapperspb.Int64Value"):
		return "*int64"
	case strings.Contains(fullName, "wrapperspb.FloatValue"):
		return "*float32"
	case strings.Contains(fullName, "wrapperspb.DoubleValue"):
		return "*float64"
	case strings.Contains(fullName, "wrapperspb.StringValue"):
		return "*string"
	case strings.Contains(fullName, "wrapperspb.BytesValue"):
		return "[]byte"
	}

	if strings.Contains(pkgPath, "envoyproxy") {
		g.queueType(t, typeName)
		return "*" + typeName
	}

	return ""
}

func (g *Generator) mapEnumType(t reflect.Type) string {
	typeName := t.Name()
	if g.enumDefs[typeName] == nil {
		g.enumDefs[typeName] = &EnumDef{Name: typeName}
	}
	return typeName
}

func (g *Generator) getJSONName(field reflect.StructField) string {
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		if parts[0] != "" && parts[0] != "-" {
			return parts[0]
		}
	}

	if protoTag := field.Tag.Get("protobuf"); protoTag != "" {
		for _, part := range strings.Split(protoTag, ",") {
			if strings.HasPrefix(part, "name=") {
				return strings.TrimPrefix(part, "name=")
			}
		}
	}

	return toSnakeCase(field.Name)
}

func (g *Generator) writeOutput() error {
	var buf strings.Builder

	buf.WriteString("// Code generated by tools/typegen/hcm. DO NOT EDIT.\n")
	buf.WriteString("// Source: github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3\n")
	buf.WriteString(fmt.Sprintf("// Envoy version: %s\n", g.envoyTag))
	buf.WriteString("// To regenerate: make generate-hcm\n\n")
	buf.WriteString("package hcm\n\n")

	buf.WriteString("import (\n")
	buf.WriteString("\t\"strings\"\n")

	importPaths := make([]string, 0, len(g.imports))
	for path := range g.imports {
		importPaths = append(importPaths, path)
	}
	sort.Strings(importPaths)

	if len(importPaths) > 0 {
		buf.WriteString("\n")
		for _, path := range importPaths {
			alias := g.imports[path]
			buf.WriteString(fmt.Sprintf("\t%s %q\n", alias, path))
		}
	}
	buf.WriteString(")\n\n")

	buf.WriteString("var _ = strings.ToUpper // suppress unused import\n\n")

	enumNames := make([]string, 0, len(g.enumDefs))
	for name := range g.enumDefs {
		enumNames = append(enumNames, name)
	}
	sort.Strings(enumNames)

	for _, name := range enumNames {
		g.writeEnum(&buf, g.enumDefs[name])
	}

	typeNames := make([]string, 0, len(g.typeDefs))
	for name := range g.typeDefs {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)

	// Put HCM first
	for i, name := range typeNames {
		if name == "HCM" {
			typeNames = append([]string{"HCM"}, append(typeNames[:i], typeNames[i+1:]...)...)
			break
		}
	}

	for _, name := range typeNames {
		g.writeStruct(&buf, g.typeDefs[name])
	}

	filename := filepath.Join(g.outputDir, "hcm_generated.go")
	return os.WriteFile(filename, []byte(buf.String()), 0o600) // #nosec G306 -- generated code file
}

func (g *Generator) writeEnum(buf *strings.Builder, e *EnumDef) {
	fmt.Fprintf(buf, "// %s is an enum type.\n", e.Name)
	fmt.Fprintf(buf, "type %s string\n\n", e.Name)

	if len(e.Values) > 0 {
		buf.WriteString("const (\n")
		for _, v := range e.Values {
			constName := fmt.Sprintf("%s_%s", e.Name, v.Name)
			fmt.Fprintf(buf, "\t%s %s = %q\n", constName, e.Name, v.Name)
		}
		buf.WriteString(")\n\n")
	}

	buf.WriteString("var (\n")
	fmt.Fprintf(buf, "\t%s_name = map[int32]string{\n", e.Name)
	for _, v := range e.Values {
		fmt.Fprintf(buf, "\t\t%d: %q,\n", v.Value, v.Name)
	}
	buf.WriteString("\t}\n")
	fmt.Fprintf(buf, "\t%s_value = map[string]int32{\n", e.Name)
	for _, v := range e.Values {
		fmt.Fprintf(buf, "\t\t%q: %d,\n", v.Name, v.Value)
	}
	buf.WriteString("\t}\n")
	buf.WriteString(")\n\n")

	fmt.Fprintf(buf, "func (x %s) ToInt() int32 {\n", e.Name)
	fmt.Fprintf(buf, "\treturn %s_value[strings.ToUpper(string(x))]\n", e.Name)
	buf.WriteString("}\n\n")
}

func (g *Generator) writeStruct(buf *strings.Builder, t *TypeDef) {
	fmt.Fprintf(buf, "// %s represents the Envoy %s configuration.\n", t.Name, t.Name)
	fmt.Fprintf(buf, "type %s struct {\n", t.Name)

	for _, f := range t.Fields {
		if strings.Contains(f.Type, "runtime.RawExtension") {
			buf.WriteString("\t// +kubebuilder:pruning:PreserveUnknownFields\n")
		}
		fmt.Fprintf(buf, "\t%s %s `json:\"%s,omitempty\"`\n", f.Name, f.Type, f.JSONName)
	}

	buf.WriteString("}\n\n")
	g.writeDeepCopy(buf, t)
}

func (g *Generator) isRecursiveField(parentType, fieldType string) bool {
	if fieldType == "*"+parentType {
		return true
	}

	if strings.HasPrefix(fieldType, "[]*") {
		elemType := strings.TrimPrefix(fieldType, "[]*")
		return g.checkRecursiveReference(parentType, elemType)
	}

	if strings.HasPrefix(fieldType, "*") {
		elemType := strings.TrimPrefix(fieldType, "*")
		return g.checkRecursiveReference(parentType, elemType)
	}

	return false
}

func (g *Generator) checkRecursiveReference(parentType, elemType string) bool {
	elemDef, exists := g.typeDefs[elemType]
	if !exists {
		return false
	}

	for _, f := range elemDef.Fields {
		if strings.Contains(f.Type, "*"+parentType) || f.Type == parentType {
			return true
		}
		innerType := strings.TrimPrefix(strings.TrimPrefix(f.Type, "[]*"), "*")
		if innerDef, ok := g.typeDefs[innerType]; ok {
			for _, innerField := range innerDef.Fields {
				if strings.Contains(innerField.Type, "*"+parentType) || innerField.Type == parentType {
					return true
				}
			}
		}
	}

	return false
}

func (g *Generator) writeDeepCopy(buf *strings.Builder, t *TypeDef) {
	buf.WriteString("// DeepCopyInto copies the receiver into out.\n")
	fmt.Fprintf(buf, "func (in *%s) DeepCopyInto(out *%s) {\n", t.Name, t.Name)
	buf.WriteString("\t*out = *in\n")

	for _, f := range t.Fields {
		g.writeDeepCopyField(buf, f)
	}

	buf.WriteString("}\n\n")
}

func (g *Generator) writeDeepCopyField(buf *strings.Builder, f FieldDef) {
	ft := f.Type
	name := f.Name

	if strings.HasPrefix(ft, "*") {
		innerType := strings.TrimPrefix(ft, "*")
		if isPrimitiveType(innerType) {
			fmt.Fprintf(buf, "\tif in.%s != nil {\n", name)
			fmt.Fprintf(buf, "\t\tin, out := &in.%s, &out.%s\n", name, name)
			fmt.Fprintf(buf, "\t\t*out = new(%s)\n", innerType)
			buf.WriteString("\t\t**out = **in\n")
			buf.WriteString("\t}\n")
			return
		}

		fmt.Fprintf(buf, "\tif in.%s != nil {\n", name)
		fmt.Fprintf(buf, "\t\tin, out := &in.%s, &out.%s\n", name, name)
		fmt.Fprintf(buf, "\t\t*out = new(%s)\n", innerType)
		buf.WriteString("\t\t(*in).DeepCopyInto(*out)\n")
		buf.WriteString("\t}\n")
		return
	}

	if strings.HasPrefix(ft, "[]") {
		elemType := strings.TrimPrefix(ft, "[]")
		fmt.Fprintf(buf, "\tif in.%s != nil {\n", name)
		fmt.Fprintf(buf, "\t\tin, out := &in.%s, &out.%s\n", name, name)
		fmt.Fprintf(buf, "\t\t*out = make(%s, len(*in))\n", ft)

		if strings.HasPrefix(elemType, "*") {
			innerType := strings.TrimPrefix(elemType, "*")
			buf.WriteString("\t\tfor i := range *in {\n")
			buf.WriteString("\t\t\tif (*in)[i] != nil {\n")
			fmt.Fprintf(buf, "\t\t\t\t(*out)[i] = new(%s)\n", innerType)
			if isPrimitiveType(innerType) {
				buf.WriteString("\t\t\t\t*(*out)[i] = *(*in)[i]\n")
			} else {
				buf.WriteString("\t\t\t\t(*in)[i].DeepCopyInto((*out)[i])\n")
			}
			buf.WriteString("\t\t\t}\n")
			buf.WriteString("\t\t}\n")
		} else {
			buf.WriteString("\t\tcopy(*out, *in)\n")
		}
		buf.WriteString("\t}\n")
		return
	}

	if strings.HasPrefix(ft, "map[") {
		fmt.Fprintf(buf, "\tif in.%s != nil {\n", name)
		fmt.Fprintf(buf, "\t\tin, out := &in.%s, &out.%s\n", name, name)
		fmt.Fprintf(buf, "\t\t*out = make(%s, len(*in))\n", ft)
		buf.WriteString("\t\tfor key, val := range *in {\n")
		// Note: This is a shallow copy of map values. For HCM types, map values
		// are typically primitives (strings, ints) rather than pointers to structs,
		// so shallow copy is sufficient. If pointer-to-struct values are needed
		// in the future, this would need to handle deep copying of values.
		buf.WriteString("\t\t\t(*out)[key] = val\n")
		buf.WriteString("\t\t}\n")
		buf.WriteString("\t}\n")
	}
}

func isPrimitiveType(t string) bool {
	switch t {
	case "bool", "string", "byte",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return true
	default:
		return false
	}
}

func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}
