# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

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
# bpfd.io/bpfd-operator-bundle:$VERSION and bpfd.io/bpfd-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= bpfd.io/bpfd-operator

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
BPFD_IMG ?= quay.io/bpfd/bpfd:main
BPFD_AGENT_IMG ?= quay.io/bpfd/bpfd-agent:main
BPFD_OPERATOR_IMG ?= quay.io/bpfd/bpfd-operator:main
BPFD_OPERATOR_BUNDLE_IMG ?= quay.io/bpfd/bpfd-operator-bundle:main

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

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.10.0
CM_VERIFIER_VERSION ?= 0.3.0

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

## Leave to my own release until https://github.com/alenkacz/cert-manager-verifier/pull/13 merges
CM_VERIFIER_BINARY ?= https://github.com/astoycos/cert-manager-verifier/releases/download/v$(CM_VERIFIER_VERSION)/cert-manager-verifier_$(CM_VERIFIER_VERSION)_Linux_$(shell go env GOARCH).tar.gz
.PHONY: cm-verifier
cm-verifier: $(CM_VERIFIER) ## Download cm-verifier locally if necessary.
$(CM_VERIFIER): $(LOCALBIN)
	test -s $(LOCALBIN)/cm_verifier || { curl -LSs $(CM_VERIFIER_BINARY) | tar -xz -C $(LOCALBIN); }

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

OUTPUT_PKG ?= github.com/redhat-et/bpfd/bpfd-operator/pkg/client
APIS_PKG ?= github.com/redhat-et/bpfd/bpfd-operator
CLIENTSET_NAME ?= versioned
CLIENTSET_PKG_NAME ?= clientset
COMMON_FLAGS ?= ${VERIFY_FLAG} --go-header-file $(shell pwd)/hack/boilerplate.go.txt

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=agent-role paths="./controllers/bpfd-agent" output:rbac:artifacts:config=config/rbac/bpfd-agent-role
	$(CONTROLLER_GEN) rbac:roleName=operator-role paths="./controllers/bpfd-operator" output:rbac:artifacts:config=config/rbac/bpfd-operator-role


.PHONY: generate
generate: generate-register generate-deepcopy generate-typed-clients generate-typed-listers generate-typed-informers ## Generate deepcopy, client, lister, and informer code

.PHONY: generate-register
generate-register:
	go run k8s.io/code-generator/cmd/register-gen \
		--input-dirs "${APIS_PKG}/apis/v1alpha1" \
		--output-package "${APIS_PKG}/apis/" \
		${COMMON_FLAGS}

.PHONY: generate-deepcopy
generate-deepcopy: ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
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

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

.PHONY: bundle
bundle: manifests kustomize ## Generate bundle manifests and metadata, then validate generated files.
	operator-sdk generate kustomize manifests -q
	cd config/bpfd-operator-deployment && $(KUSTOMIZE) edit set image bpfd-operator-image=${BPFD_OPERATOR_IMG}
	cd config/bpfd-deployment && $(KUSTOMIZE) edit set image bpfd-image=${BPFD_IMG} && $(KUSTOMIZE) edit set image bpfd-agent-image=${BPFD_AGENT_IMG}
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle $(BUNDLE_GEN_FLAGS)
	operator-sdk bundle validate ./bundle

##@ Build

.PHONY: build
build: generate fmt vet ## Build bpfd-operator and bpfd-agent binaries.
	go build -o bin/bpfd-operator cmd/bpfd-operator/main.go
	go build -o bin/bpfd-agent cmd/bpfd-agent/main.go

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: build-images
build-images: ## Build bpfd, bpfd-agent, and bpfd-operator images.
	docker build -t ${BPFD_OPERATOR_IMG} -f Containerfile.bpfd-operator ../
	docker build -t ${BPFD_AGENT_IMG} -f Containerfile.bpfd-agent ../
	DOCKER_BUILDKIT=1 docker build -t ${BPFD_IMG} -f ../packaging/container-deployment/Containerfile.bpfd.local ../

.PHONY: push-images
push-images: ## Push bpfd, bpfd-agent, bpfd-operator images.
	docker push ${BPFD_OPERATOR_IMG}
	docker push ${BPFD_AGENT_IMG}
	docker push ${BPFD_IMG}

.PHONY: load-images-kind
load-images-kind: build-images ## Load bpfd, bpfd-agent, and bpfd-operator images into the running local kind devel cluster.
	kind load docker-image  ${BPFD_IMG} ${BPFD_OPERATOR_IMG} ${BPFD_AGENT_IMG} --name bpfd-deployment

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f Containerfile.bundle -t $(BPFD_OPERATOR_BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	docker push $(BPFD_OPERATOR_BUNDLE_IMG)

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

##@ Deployment Dependencies

.PHONY: deploy-certmanager
deploy-certmanager: kustomize cm-verifier ## Deploy certmanager to the cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/certmanager | kubectl apply -f -
	$(CM_VERIFIER) --timeout 60s

.PHONY: undeploy-certmanager
undeploy-certmanager: ## Undeploy certmanager from the cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/certmanager  | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Vanilla K8s Deployment

.PHONY: setup-kind
setup-kind: cm-verifier ## Setup Kind cluster with certmanager ready for bpfd deployment
	kind delete cluster --name bpfd-deployment && kind create cluster --config hack/kind-config.yaml --name bpfd-deployment

## Default deploy target is KIND based.
.PHONY: deploy
deploy: manifests kustomize cm-verifier ## Deploy bpfd-operator to the K8s cluster specified in ~/.kube/config.
	cd config/bpfd-operator-deployment && $(KUSTOMIZE) edit set image bpfd-operator-image=${BPFD_OPERATOR_IMG} 
	cd config/bpfd-deployment && $(KUSTOMIZE) edit set image bpfd-image=${BPFD_IMG} && $(KUSTOMIZE) edit set image bpfd-agent-image=${BPFD_AGENT_IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy bpfd-operator from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: kind-reload-images
kind-reload-images: load-images-kind ## Reload locally build images into a kind cluster and restart the ds and deployment so they're picked up.
	kubectl rollout restart daemonset bpfd-daemon -n bpfd
	kubectl rollout restart deployment bpfd-operator -n bpfd

.PHONY: run-on-kind
run-on-kind: kustomize setup-kind deploy-certmanager load-images-kind deploy ## Kind Deploy runs the bpfd-operator on a local kind cluster using local builds of bpfd, bpfd-agent, and bpfd-operator

##@ Openshift Deployment

.PHONY: deploy-openshift
deploy-openshift: manifests kustomize ## Deploy bpfd-operator to the Openshift cluster specified in ~/.kube/config.
	cd config/bpfd-operator-deployment && $(KUSTOMIZE) edit set image bpfd-operator-image=${BPFD_OPERATOR_IMG} 
	cd config/bpfd-deployment && $(KUSTOMIZE) edit set image bpfd-image=${BPFD_IMG} && $(KUSTOMIZE) edit set image bpfd-agent-image=${BPFD_AGENT_IMG}
	$(KUSTOMIZE) build config/openshift | kubectl apply -f -

.PHONY: undeploy-openshift
undeploy-openshift: ## Undeploy bpfd-operator from the Openshift cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/openshift | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Testing

.PHONY: test-integration
test-integration: manifests generate fmt vet
	go clean -testcache
	GOFLAGS="-tags=integration_tests" go test -race -v ./test/integration/...
