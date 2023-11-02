# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.3.1

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# bpfman.io/bpfman-operator-bundle:$VERSION and bpfman.io/bpfman-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= bpfman.io/bpfman-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Image URL to use all building/pushing image targets
BPFMAN_IMG ?= quay.io/bpfman/bpfman:latest
BPFMAN_AGENT_IMG ?= quay.io/bpfman/bpfman-agent:latest
BPFMAN_OPERATOR_IMG ?= quay.io/bpfman/bpfman-operator:latest
BPFMAN_OPERATOR_BUNDLE_IMG ?= quay.io/bpfman/bpfman-operator-bundle:latest
KIND_CLUSTER_NAME ?= bpfman-deployment

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.25.0

.DEFAULT_GOAL := help

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

##@ Local Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
CM_VERIFIER ?= $(LOCALBIN)/cm-verifier
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.11.3
OPERATOR_SDK_VERSION ?= v1.27.0

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

OPERATOR_SDK_DL_NAME=operator-sdk_$(shell go env GOOS)_$(shell go env GOARCH)
OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/$(OPERATOR_SDK_DL_NAME)
.PHONY: operator-sdk
operator-sdk: $(OPERATOR_SDK)
$(OPERATOR_SDK): $(LOCALBIN)
	test -s $(LOCALBIN)/operator_sdk || { curl -LO ${OPERATOR_SDK_DL_URL} && chmod +x ${OPERATOR_SDK_DL_NAME} && mv ${OPERATOR_SDK_DL_NAME} $(LOCALBIN)/operator-sdk; }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

##@ Development

VERIFY_CODEGEN ?= false
ifeq ($(VERIFY_CODEGEN), true)
VERIFY_FLAG=--verify-only
endif

OUTPUT_PKG ?= github.com/bpfman/bpfman/bpfman-operator/pkg/client
APIS_PKG ?= github.com/bpfman/bpfman/bpfman-operator
CLIENTSET_NAME ?= versioned
CLIENTSET_PKG_NAME ?= clientset
COMMON_FLAGS ?= ${VERIFY_FLAG} --go-header-file $(shell pwd)/hack/boilerplate.go.txt

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=agent-role paths="./controllers/bpfman-agent/..." output:rbac:artifacts:config=config/rbac/bpfman-agent
	$(CONTROLLER_GEN) rbac:roleName=operator-role paths="./controllers/bpfman-operator" output:rbac:artifacts:config=config/rbac/bpfman-operator

.PHONY: generate
generate: manifests generate-register generate-deepcopy generate-typed-clients generate-typed-listers generate-typed-informers ## Generate ALL auto-generated code.

.PHONY: generate-register
generate-register: ## Generate register code see all `zz_generated.register.go` files.
	go run k8s.io/code-generator/cmd/register-gen \
		--input-dirs "${APIS_PKG}/apis/v1alpha1" \
		--output-package "${APIS_PKG}/apis/" \
		${COMMON_FLAGS}

.PHONY: generate-deepcopy
generate-deepcopy: ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations see all `zz_generated.register.go` files.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: generate-typed-clients
generate-typed-clients: ## Generate typed client code
	go run k8s.io/code-generator/cmd/client-gen \
	--clientset-name "${CLIENTSET_NAME}" \
	--input-base "" \
	--input "${APIS_PKG}/apis/v1alpha1" \
	--output-package "${OUTPUT_PKG}/${CLIENTSET_PKG_NAME}" \
	${COMMON_FLAGS}


.PHONY: generate-typed-listers
generate-typed-listers: ## Generate typed listers code
	go run k8s.io/code-generator/cmd/lister-gen \
	--input-dirs "${APIS_PKG}/apis/v1alpha1" \
	--output-package "${OUTPUT_PKG}/listers" \
	${COMMON_FLAGS}


.PHONY: generate-typed-informers
generate-typed-informers: ## Generate typed informers code
	go run k8s.io/code-generator/cmd/informer-gen \
	--input-dirs "${APIS_PKG}/apis/v1alpha1" \
	--versioned-clientset-package "${OUTPUT_PKG}/${CLIENTSET_PKG_NAME}/${CLIENTSET_NAME}" \
	--listers-package "${OUTPUT_PKG}/listers" \
	--output-package "${OUTPUT_PKG}/informers" \
	${COMMON_FLAGS}

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: verify
verify: ## Verify all the autogenerated code
	./hack/verify-codegen.sh

# Use bpfman/scripts/verify-golint.sh for local linting verification
# .PHONY: lint
# lint: ## Run golang-ci linter
# 	./hack/verify-golint.sh

.PHONY: test
test: fmt envtest ## Run Unit tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out


.PHONY: test-integration
test-integration: ## Run Integration tests.
	go clean -testcache
	GOFLAGS="-tags=integration_tests" go test -race -v ./test/integration/...

## The physical bundle is no longer tracked in git since it should be considered
## and treated as a release artifact, rather than something that's updated 
## as part of a pull request.
## See https://github.com/operator-framework/operator-sdk/issues/6285.
.PHONY: bundle
bundle: operator-sdk generate kustomize manifests ## Generate bundle manifests and metadata, then validate generated files.
	cd config/bpfman-operator-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-operator=${BPFMAN_OPERATOR_IMG}
	cd config/bpfman-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman=${BPFMAN_IMG} && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-agent=${BPFMAN_AGENT_IMG}
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: build-release-yamls
build-release-yamls: generate kustomize ## Generate the crd install bundle for a specific release version.
	VERSION=$(VERSION) ./hack/build-release-yamls.sh

##@ Build

.PHONY: build
build: fmt ## Build bpfman-operator and bpfman-agent binaries.
	go build -o bin/bpfman-operator cmd/bpfman-operator/main.go
	go build -o bin/bpfman-agent cmd/bpfman-agent/main.go

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: build-images
build-images: ## Build bpfman, bpfman-agent, and bpfman-operator images.
	docker build -t ${BPFMAN_OPERATOR_IMG} -f Containerfile.bpfman-operator ../
	docker build -t ${BPFMAN_AGENT_IMG} -f Containerfile.bpfman-agent ../
	DOCKER_BUILDKIT=1 docker build -t ${BPFMAN_IMG} -f ../Containerfile.bpfman.local ../

.PHONY: push-images
push-images: ## Push bpfman, bpfman-agent, bpfman-operator images.
	docker push ${BPFMAN_OPERATOR_IMG}
	docker push ${BPFMAN_AGENT_IMG}
	docker push ${BPFMAN_IMG}

.PHONY: load-images-kind
load-images-kind: ## Load bpfman, bpfman-agent, and bpfman-operator images into the running local kind devel cluster.
	kind load docker-image  ${BPFMAN_IMG} ${BPFMAN_OPERATOR_IMG} ${BPFMAN_AGENT_IMG} --name ${KIND_CLUSTER_NAME}

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f Containerfile.bundle -t $(BPFMAN_OPERATOR_BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	docker push $(BPFMAN_OPERATOR_BUNDLE_IMG)

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	docker push $(CATALOG_IMG)

##@ CRD Deployment

ignore-not-found ?= false

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Vanilla K8s Deployment

.PHONY: setup-kind
setup-kind: ## Setup Kind cluster
	kind delete cluster --name ${KIND_CLUSTER_NAME} && kind create cluster --config hack/kind-config.yaml --name ${KIND_CLUSTER_NAME}

## Default deploy target is KIND based with its CSI driver initialized.
.PHONY: deploy
deploy: manifests kustomize ## Deploy bpfman-operator to the K8s cluster specified in ~/.kube/config with the csi driver initialized.
	cd config/bpfman-operator-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-operator=${BPFMAN_OPERATOR_IMG}
	cd config/bpfman-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman=${BPFMAN_IMG} && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-agent=${BPFMAN_AGENT_IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy bpfman-operator from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: kind-reload-images
kind-reload-images: load-images-kind ## Reload locally build images into a kind cluster and restart the ds and deployment so they're picked up.
	kubectl rollout restart daemonset bpfman-daemon -n bpfman
	kubectl rollout restart deployment bpfman-operator -n bpfman

.PHONY: run-on-kind
run-on-kind: kustomize setup-kind build-images load-images-kind deploy ## Kind Deploy runs the bpfman-operator on a local kind cluster using local builds of bpfman, bpfman-agent, and bpfman-operator

##@ Openshift Deployment

.PHONY: deploy-openshift
deploy-openshift: manifests kustomize ## Deploy bpfman-operator to the Openshift cluster specified in ~/.kube/config.
	cd config/bpfman-operator-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-operator=${BPFMAN_OPERATOR_IMG}
	cd config/bpfman-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman=${BPFMAN_IMG} && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-agent=${BPFMAN_AGENT_IMG}
	$(KUSTOMIZE) build config/openshift | kubectl apply -f -

.PHONY: undeploy-openshift
undeploy-openshift: ## Undeploy bpfman-operator from the Openshift cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/openshift | kubectl delete --ignore-not-found=$(ignore-not-found) -f -
