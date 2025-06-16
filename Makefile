# Copyright 2023 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

CONTAINER_TOOL ?= docker
MKDIR    ?= mkdir
TR       ?= tr
DIST_DIR ?= $(CURDIR)/dist
HELM     ?= "go run helm.sh/helm/v3/cmd/helm@latest"

export IMAGE_GIT_TAG ?= $(shell git describe --tags --always --dirty --match 'v*')
export CHART_GIT_TAG ?= $(shell git describe --tags --always --dirty --match 'chart/*')

include $(CURDIR)/common.mk

BUILDIMAGE_TAG ?= golang$(GOLANG_VERSION)
BUILDIMAGE ?= $(IMAGE_NAME)-build:$(BUILDIMAGE_TAG)

CMDS := $(patsubst ./cmd/%/,%,$(sort $(dir $(wildcard ./cmd/*/))))
CMD_TARGETS := $(patsubst %,cmd-%, $(CMDS))

CHECK_TARGETS := assert-fmt vet lint ineffassign misspell
MAKE_TARGETS := binaries build check vendor fmt test examples cmds coverage generate $(CHECK_TARGETS)

TARGETS := $(MAKE_TARGETS) $(CMD_TARGETS)

DOCKER_TARGETS := $(patsubst %,docker-%, $(TARGETS))
.PHONY: $(TARGETS) $(DOCKER_TARGETS)

GOOS ?= linux

binaries: cmds
ifneq ($(PREFIX),)
cmd-%: COMMAND_BUILD_OPTIONS = -o $(PREFIX)/$(*)
endif
cmds: $(CMD_TARGETS)
$(CMD_TARGETS): cmd-%:
	CGO_LDFLAGS_ALLOW='-Wl,--unresolved-symbols=ignore-in-object-files' GOOS=$(GOOS) \
		go build -ldflags "-s -w -X main.version=$(VERSION)" $(COMMAND_BUILD_OPTIONS) $(MODULE)/cmd/$(*)

build:
	GOOS=$(GOOS) go build ./...

examples: $(EXAMPLE_TARGETS)
$(EXAMPLE_TARGETS): example-%:
	GOOS=$(GOOS) go build ./examples/$(*)

all: check test build binary
check: $(CHECK_TARGETS)

# Update the vendor folder
vendor:
	go mod vendor

# Apply go fmt to the codebase
fmt:
	go list -f '{{.Dir}}' $(MODULE)/... \
		| xargs gofmt -s -l -w

assert-fmt:
	go list -f '{{.Dir}}' $(MODULE)/... \
		| xargs gofmt -s -l > fmt.out
	@if [ -s fmt.out ]; then \
		echo "\nERROR: The following files are not formatted:\n"; \
		cat fmt.out; \
		rm fmt.out; \
		exit 1; \
	else \
		rm fmt.out; \
	fi

ineffassign:
	ineffassign $(MODULE)/...

lint:
	golangci-lint run ./...

misspell:
	misspell $(MODULE)/...

vet:
	go vet $(MODULE)/...

# Ensure that all log calls support contextual logging.
test: logcheck
.PHONY: logcheck
logcheck:
	(cd hack/tools && GOBIN=$(PWD) go install sigs.k8s.io/logtools/logcheck)
	./logcheck -check-contextual -check-deprecations ./...

COVERAGE_FILE := coverage.out
test: build cmds
	go test -v -coverprofile=$(COVERAGE_FILE) $(MODULE)/...

coverage: test
	cat $(COVERAGE_FILE) | grep -v "_mock.go" > $(COVERAGE_FILE).no-mocks
	go tool cover -func=$(COVERAGE_FILE).no-mocks

generate: generate-deepcopy

generate-deepcopy: vendor
	for api in $(APIS); do \
		rm -f $(CURDIR)/api/$(VENDOR)/resource/$${api}/zz_generated.deepcopy.go; \
		controller-gen \
			object:headerFile=$(CURDIR)/hack/boilerplate.generatego.txt \
			paths=$(CURDIR)/api/$(VENDOR)/resource/$${api}/ \
			output:object:dir=$(CURDIR)/api/$(VENDOR)/resource/$${api}; \
	done

setup-e2e:
	test/e2e/setup-e2e.sh

test-e2e:
	test/e2e/e2e.sh

teardown-e2e:
	test/e2e/teardown-e2e.sh

# Generate an image for containerized builds
# Note: This image is local only
.PHONY: .build-image
.build-image: docker/Dockerfile.devel
	if [ x"$(SKIP_IMAGE_BUILD)" = x"" ]; then \
		$(CONTAINER_TOOL) build \
			--progress=plain \
			--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
			--tag $(BUILDIMAGE) \
			-f $(^) \
			docker; \
	fi

ifeq ($(CONTAINER_TOOL),podman)
CONTAINER_TOOL_OPTS=-v $(PWD):$(PWD):Z
else
CONTAINER_TOOL_OPTS=-v $(PWD):$(PWD) --user $$(id -u):$$(id -g)
endif

$(DOCKER_TARGETS): docker-%: .build-image
	@echo "Running 'make $(*)' in container $(BUILDIMAGE)"
	$(CONTAINER_TOOL) run \
		--rm \
		-e HOME=$(PWD) \
		-e GOCACHE=$(PWD)/.cache/go \
		-e GOPATH=$(PWD)/.cache/gopath \
		$(CONTAINER_TOOL_OPTS) \
		-w $(PWD) \
		$(BUILDIMAGE) \
			make $(*)

# Start an interactive shell using the development image.
.PHONY: .shell
.shell:
	$(CONTAINER_TOOL) run \
		--rm \
		-ti \
		-e HOME=$(PWD) \
		-e GOCACHE=$(PWD)/.cache/go \
		-e GOPATH=$(PWD)/.cache/gopath \
		$(CONTAINER_TOOL_OPTS) \
		-w $(PWD) \
		$(BUILDIMAGE)

.PHONY: push-release-artifacts
push-release-artifacts:
	CHART_VERSION="$${CHART_GIT_TAG##chart/}" \
		HELM=$(HELM) \
		demo/scripts/push-driver-chart.sh
	export DRIVER_IMAGE_TAG="${IMAGE_GIT_TAG}"; \
	demo/scripts/build-driver-image.sh && \
	demo/scripts/push-driver-image.sh
