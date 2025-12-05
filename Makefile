
# Image URL to use all building/pushing image targets
IMG ?= local/xds:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.25.0

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
VERSION_PKG = github.com/tentens-tech/xds-controller/pkg/version
LDFLAGS = -X $(VERSION_PKG).Version=$(VERSION) \
          -X $(VERSION_PKG).GitCommit=$(GIT_COMMIT) \
          -X $(VERSION_PKG).BuildDate=$(BUILD_DATE)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd:allowDangerousTypes=true webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Envoy module version for type generation (should match go.mod)
ENVOY_TAG ?= v1.36.0

.PHONY: generate-types
generate-types: generate-cds generate-lds generate-rds generate-eds ## Generate all xDS types from go-control-plane
	@echo "Type generation complete. Run 'make generate' to regenerate DeepCopy methods."

.PHONY: generate-cds
generate-cds: ## Generate CDS (Cluster) types from go-control-plane
	@echo "Generating CDS types from envoy $(ENVOY_TAG)..."
	go run ./tools/typegen/cds -output pkg/xds/types/cds -envoy-tag $(ENVOY_TAG) -v
	gofmt -w pkg/xds/types/cds/

.PHONY: generate-lds
generate-lds: ## Generate LDS (Listener) types from go-control-plane
	@echo "Generating LDS types from envoy $(ENVOY_TAG)..."
	go run ./tools/typegen/lds -output pkg/xds/types/lds -envoy-tag $(ENVOY_TAG) -v
	gofmt -w pkg/xds/types/lds/

.PHONY: generate-rds
generate-rds: ## Generate RDS (Route) types from go-control-plane
	@echo "Generating RDS types from envoy $(ENVOY_TAG)..."
	go run ./tools/typegen/rds -output pkg/xds/types/rds -envoy-tag $(ENVOY_TAG) -v
	gofmt -w pkg/xds/types/rds/

.PHONY: generate-eds
generate-eds: ## Generate EDS (Endpoint) types from go-control-plane
	@echo "Generating EDS types from envoy $(ENVOY_TAG)..."
	go run ./tools/typegen/eds -output pkg/xds/types/eds -envoy-tag $(ENVOY_TAG) -v
	gofmt -w pkg/xds/types/eds/

.PHONY: generateall
generateall: generate-types generate manifests ## Run full generation: types -> deepcopy -> CRDs
	@echo "Full generation complete!"
	@echo "You can now apply CRDs with: kubectl apply -f config/crd/bases/"

.PHONY: mockgen
mockgen: mockgen-install ## Generate mock code for testing
	$(LOCALBIN)/mockgen -source ./pkg/xds/sds.go -destination ./pkg/xds/mock/sds.go

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint against code.
	golangci-lint run --timeout=5m

.PHONY: pre-commit
pre-commit: fmt vet lint ## Run pre-commit checks (fmt, vet, lint).
	@echo "Pre-commit checks passed!"

.PHONY: pre-commit-install
pre-commit-install: ## Install pre-commit hooks.
	@command -v pre-commit >/dev/null 2>&1 || { echo "Installing pre-commit..."; pip install pre-commit; }
	pre-commit install
	@echo "Pre-commit hooks installed!"

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

.PHONY: cover
cover: ## Run tests with coverage report (excludes generated code)
	@# Note: "no such tool covdata" warnings are expected for packages without tests (Go 1.20+)
	-@go test -short -count=1 -race -coverprofile=coverage.raw.out ./... 2>&1 | { grep -v 'no such tool "covdata"' || true; }
	@echo "Filtering out generated files from coverage..."
	@if [ -f coverage.raw.out ]; then \
		grep -v '_generated\.go\|zz_generated\|/mock/' coverage.raw.out > coverage.out || cp coverage.raw.out coverage.out; \
		go tool cover -func=coverage.out | grep total; \
		go tool cover -html=coverage.out -o coverage.html; \
		echo "Coverage report: coverage.html"; \
		rm -f coverage.raw.out; \
	else \
		echo "No coverage data generated"; \
	fi

.PHONY: test-e2e
test-e2e: ## Run end-to-end tests with KIND cluster
	chmod +x test/e2e/run-e2e.sh
	./test/e2e/run-e2e.sh

.PHONY: test-e2e-local
test-e2e-local: docker-build ## Run E2E tests using local docker image
	@echo "Building and loading image..."
	docker tag ${IMG} xds-controller:e2e
	kind create cluster --name xds-e2e --wait 60s || true
	kind load docker-image xds-controller:e2e --name xds-e2e
	@echo "Applying resources..."
	kubectl apply -f config/crd/bases/
	kubectl apply -f test/e2e/manifests/
	@echo "Waiting for deployments..."
	kubectl rollout status deployment/xds-controller -n xds-system --timeout=120s
	kubectl rollout status deployment/test-backend -n xds-system --timeout=60s
	kubectl rollout status deployment/envoy -n xds-system --timeout=120s
	@echo "E2E environment ready. Run 'make test-e2e-cleanup' when done."

.PHONY: test-e2e-cleanup
test-e2e-cleanup: ## Cleanup KIND cluster from E2E tests
	kind delete cluster --name xds-e2e

##@ Build

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build -ldflags "$(LDFLAGS)" -o bin/xds ./cmd/xds/main.go

.PHONY: build-quick
build-quick: ## Build manager binary without code generation (faster for development).
	go build -ldflags "$(LDFLAGS)" -o bin/xds ./cmd/xds/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run -ldflags "$(LDFLAGS)" ./cmd/xds/main.go

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: #test ## Build docker image with the manager.
	docker build -f deployment/docker/Dockerfile \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

.PHONY: docker-bp
docker-bp: docker-build ## Push docker image with the manager.
	docker push ${IMG}

# PLATFORMS defines the target platforms for  the manager image be build to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - able to use docker buildx . More info: https://docs.docker.com/build/buildx/
# - have enable BuildKit, More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image for your registry (i.e. if you do not inform a valid value via IMG=<myregistry/image:<tag>> than the export will fail)
# To properly provided solutions that supports more than one platform you should use this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: test ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' deployment/docker/Dockerfile > Dockerfile.cross
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	- docker buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross
	- docker buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
MOCKGEN ?= $(LOCALBIN)/mockgen
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v4.5.5
CONTROLLER_TOOLS_VERSION ?= v0.16.4
GOLANGCI_LINT_VERSION ?= v2.7.1

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: mockgen-install
mockgen-install: $(MOCKGEN)
$(MOCKGEN): $(LOCALBIN)
	test -s $(LOCALBIN)/mockgen || GOBIN=$(LOCALBIN) go install github.com/golang/mock/mockgen@v1.6.0

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	test -s $(LOCALBIN)/golangci-lint || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(LOCALBIN) $(GOLANGCI_LINT_VERSION)
