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

##### Global variables #####
CONTAINER_TOOL ?= docker
MKDIR ?= mkdir
DISTRIBUTIONS := ubuntu22.04
DOCKERFILE := $(CURDIR)/deployments/container/Dockerfile
SELF_MAKEFILE := $(lastword $(MAKEFILE_LIST))

include $(CURDIR)/common.mk

# Provide a usable default tag for local container builds.
ifeq ($(strip $(VERSION)),)
VERSION := latest
endif

IMAGE_TAG := $(VERSION)
IMAGE = $(IMAGE_NAME):$(IMAGE_TAG)
BASE_IMAGE ?= docker.io/ubuntu:22.04

# Only detect host architecture when PLATFORMS is not provided explicitly.
ifeq ($(strip $(PLATFORMS)),)
	# Detect host CPU architecture via `uname -m` and map it to a Docker platform
	# used by build (`linux/amd64` on x86_64, `linux/arm64` on arm64/aarch64).
	UNAME_M ?= $(shell uname -m)
	ifeq ($(UNAME_M),x86_64)
		PLATFORMS := linux/amd64
	else ifeq ($(UNAME_M),arm64)
		PLATFORMS := linux/arm64
	else ifeq ($(UNAME_M),aarch64)
		PLATFORMS := linux/arm64
	else
		$(error Unsupported local platform: $(UNAME_M))
	endif
endif

# Split PLATFORMS on commas; more than one entry implies a multi-platform (buildx) build.
PLATFORM_LIST := $(subst $(shell printf ','), ,$(PLATFORMS))

BUILDX_BUILDER ?= dra-example-driver-builder

# Default to disabled so release builds are stable across older buildx setups.
# Can be overridden by CI/release jobs that require attestations.
BUILDX_PROVENANCE ?= false
# Default to disabled to avoid extra metadata generation unless explicitly needed.
BUILDX_SBOM ?= false

##### Public rules #####
.PHONY: $(DISTRIBUTIONS) build push ensure-buildx-builder

ubuntu22.04: BASE_IMAGE = docker.io/ubuntu:22.04

# Use a generic build target to build the relevant images
$(DISTRIBUTIONS):
	$(MAKE) -f $(SELF_MAKEFILE) build

build:
# Multi-arch builds must use buildx (docker only).
ifneq ($(words $(PLATFORM_LIST)),1)
	@echo "Building image for multiple platforms with buildx: $(PLATFORMS)"
	@if [ "$(CONTAINER_TOOL)" != "docker" ]; then \
		echo "Multi-platform build requires CONTAINER_TOOL=docker, got $(CONTAINER_TOOL)"; \
		exit 1; \
	fi
	@echo "Note: multi-platform build does not load image into local Docker daemon."
	$(CONTAINER_TOOL) buildx build \
		--pull \
		--tag $(IMAGE) \
		--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
		--build-arg BASE_IMAGE="$(BASE_IMAGE)" \
		--build-arg VERSION="$(VERSION)" \
		-f $(DOCKERFILE) \
		$(CURDIR) \
		--platform $(PLATFORMS)
else
	# Single-arch builds use the standard container build flow.
	@echo "Building image for single platform: $(PLATFORMS)"
	$(CONTAINER_TOOL) build --pull \
		--tag $(IMAGE) \
		--build-arg GO_VERSION="$(GO_VERSION)" \
		--build-arg BASE_IMAGE="$(BASE_IMAGE)" \
		--build-arg VERSION="$(VERSION)" \
		-f $(DOCKERFILE) \
		$(CURDIR)
endif

push:
# Multi-arch pushes publish a multi-platform manifest via buildx.
ifneq ($(words $(PLATFORM_LIST)),1)
	@if [ "$(CONTAINER_TOOL)" != "docker" ]; then \
		echo "Multi-platform push requires CONTAINER_TOOL=docker, got $(CONTAINER_TOOL)"; \
		exit 1; \
	fi
	@echo "Pushing image for multiple platforms with buildx: $(PLATFORMS)"
	$(MAKE) -f $(SELF_MAKEFILE) ensure-buildx-builder
	$(CONTAINER_TOOL) buildx build \
		--pull \
		--tag $(IMAGE) \
		--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
		--build-arg BASE_IMAGE="$(BASE_IMAGE)" \
		--build-arg VERSION="$(VERSION)" \
		-f $(DOCKERFILE) \
		$(CURDIR) \
		--builder $(BUILDX_BUILDER) \
		--platform $(PLATFORMS) \
		--provenance=$(BUILDX_PROVENANCE) \
		--sbom=$(BUILDX_SBOM) \
		--push
else
	# Single-arch pushes build locally first, then push the image tag.
	$(MAKE) -f $(SELF_MAKEFILE) build
	$(CONTAINER_TOOL) push $(IMAGE)
endif

ensure-buildx-builder:
ifeq ($(CONTAINER_TOOL),docker)
	@docker buildx inspect $(BUILDX_BUILDER) >/dev/null 2>&1 || \
		docker buildx create --name $(BUILDX_BUILDER) --driver docker-container --use
	@docker buildx use $(BUILDX_BUILDER)
	@docker buildx inspect --bootstrap $(BUILDX_BUILDER) >/dev/null
else
	@echo "ensure-buildx-builder requires CONTAINER_TOOL=docker"
	@exit 1
endif
