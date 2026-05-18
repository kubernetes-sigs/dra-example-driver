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
COMMA := ,

include $(CURDIR)/common.mk

# Provide a usable default tag for local container builds.
ifeq ($(strip $(VERSION)),)
VERSION := latest
endif

IMAGE_TAG := $(VERSION)
IMAGE = $(IMAGE_NAME):$(IMAGE_TAG)
BASE_IMAGE ?= docker.io/ubuntu:22.04

# Map host CPU to a Docker platform.
UNAME_M ?= $(shell uname -m)
ifeq ($(UNAME_M),x86_64)
	HOST_PLATFORM := linux/amd64
else ifeq ($(UNAME_M),arm64)
	HOST_PLATFORM := linux/arm64
else ifeq ($(UNAME_M),aarch64)
	HOST_PLATFORM := linux/arm64
else ifeq ($(UNAME_M),ppc64le)
	HOST_PLATFORM := linux/ppc64le
else
	$(error Unsupported local platform: $(UNAME_M))
endif

# Default PLATFORMS to the host when unset. An explicit non-empty value overrides
# detection; an explicit empty value is treated as an error to avoid confusing
# Docker/buildx failures.
ifeq ($(origin PLATFORMS), undefined)
  PLATFORMS := $(HOST_PLATFORM)
else ifeq ($(strip $(PLATFORMS)),)
  $(error PLATFORMS is set but empty. Unset it to use the host platform, or provide one or more values)
endif

BUILDX_BUILDER ?= dra-example-driver-builder

# Default to disabled so release builds are stable across older buildx setups.
# Can be overridden by CI/release jobs that require attestations.
BUILDX_PROVENANCE ?= false
# Default to disabled to avoid extra metadata generation unless explicitly needed.
BUILDX_SBOM ?= false
BUILDX_METADATA_FLAGS = \
	--provenance=$(BUILDX_PROVENANCE) \
	--sbom=$(BUILDX_SBOM)

# Shared docker build / buildx arguments (expanded at use time so target-specific
# overrides such as BASE_IMAGE on distribution targets are respected).
IMAGE_BUILD_ARGS = \
	--build-arg GO_VERSION="$(GO_VERSION)" \
	--build-arg BASE_IMAGE="$(BASE_IMAGE)" \
	--build-arg VERSION="$(VERSION)"

IMAGE_BUILD_FLAGS = \
	--pull \
	--tag $(IMAGE) \
	$(IMAGE_BUILD_ARGS) \
	-f $(DOCKERFILE) \
	$(CURDIR)

BUILDX_BUILD_FLAGS = \
	$(IMAGE_BUILD_FLAGS) \
	--builder $(BUILDX_BUILDER) \
	$(BUILDX_METADATA_FLAGS) \
	--platform $(PLATFORMS)

##### Public rules #####
.PHONY: $(DISTRIBUTIONS) build push ensure-buildx-builder

ubuntu22.04: BASE_IMAGE = docker.io/ubuntu:22.04

# Use a generic build target to build the relevant images
$(DISTRIBUTIONS):
	$(MAKE) -f $(SELF_MAKEFILE) build BASE_IMAGE="$(BASE_IMAGE)"

build:
# Multi-arch builds must use buildx (docker only).
ifneq ($(findstring $(COMMA),$(PLATFORMS)),)
	@echo "Building image for multiple platforms with buildx: $(PLATFORMS)"
	@if [ "$(CONTAINER_TOOL)" != "docker" ]; then \
		echo "Multi-platform build requires CONTAINER_TOOL=docker, got $(CONTAINER_TOOL)"; \
		exit 1; \
	fi
	@echo "Note: multi-platform build does not load image into local Docker daemon."
	$(MAKE) -f $(SELF_MAKEFILE) ensure-buildx-builder
	$(CONTAINER_TOOL) buildx build $(BUILDX_BUILD_FLAGS)
else ifeq ($(PLATFORMS),$(HOST_PLATFORM))
	# Native single-arch build for the host platform.
	@echo "Building image for single platform: $(PLATFORMS)"
	$(if $(filter docker,$(CONTAINER_TOOL)),DOCKER_BUILDKIT=1 ,)$(CONTAINER_TOOL) build --platform $(PLATFORMS) $(IMAGE_BUILD_FLAGS)
else
	# Cross-arch single-platform build (e.g. linux/arm64 on x86_64): use buildx --load
	# so --platform is honoured; requires QEMU/binfmt on Linux amd64 (see README).
	@echo "Building image for single platform: $(PLATFORMS) (host is $(HOST_PLATFORM))"
	@if [ "$(CONTAINER_TOOL)" != "docker" ]; then \
		echo "Cross-platform single-arch build requires CONTAINER_TOOL=docker, got $(CONTAINER_TOOL)"; \
		exit 1; \
	fi
	@echo "Note: cross-platform build uses buildx and loads the image into the local Docker daemon."
	$(MAKE) -f $(SELF_MAKEFILE) ensure-buildx-builder
	$(CONTAINER_TOOL) buildx build $(BUILDX_BUILD_FLAGS) --load
endif

push:
# Multi-arch pushes publish a multi-platform manifest via buildx.
ifneq ($(findstring $(COMMA),$(PLATFORMS)),)
	@if [ "$(CONTAINER_TOOL)" != "docker" ]; then \
		echo "Multi-platform push requires CONTAINER_TOOL=docker, got $(CONTAINER_TOOL)"; \
		exit 1; \
	fi
	@echo "Pushing image for multiple platforms with buildx: $(PLATFORMS)"
	$(MAKE) -f $(SELF_MAKEFILE) ensure-buildx-builder
	$(CONTAINER_TOOL) buildx build $(BUILDX_BUILD_FLAGS) --push
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
