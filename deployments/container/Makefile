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
PLATFORMS ?= $(LOCAL_PLATFORM)
MULTIARCH_PLATFORMS ?= linux/amd64,linux/arm64

# Detect host CPU architecture via `uname -m` and map it to a Docker platform
# used by build-local (`linux/amd64` on x86_64, `linux/arm64` on arm64/aarch64).
UNAME_M ?= $(shell uname -m)
ifeq ($(UNAME_M),x86_64)
	LOCAL_PLATFORM ?= linux/amd64
else ifeq ($(UNAME_M),arm64)
	LOCAL_PLATFORM ?= linux/arm64
else ifeq ($(UNAME_M),aarch64)
	LOCAL_PLATFORM ?= linux/arm64
else
	LOCAL_PLATFORM ?= linux/amd64
endif

BUILDX_BUILDER ?= dra-example-driver-builder
BUILDX_PROVENANCE ?= false
BUILDX_SBOM ?= false
BUILDX_COMMON_ARGS = --pull \
	--tag $(IMAGE) \
	--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
	--build-arg BASE_IMAGE="docker.io/ubuntu:22.04" \
	--build-arg VERSION="$(VERSION)" \
	-f $(DOCKERFILE) \
	$(CURDIR)

##### Public rules #####
.PHONY: $(DISTRIBUTIONS) build push build-local push-multiarch ensure-buildx-builder

ubuntu22.04: BASE_IMAGE = docker.io/ubuntu:22.04

# Use a generic build target to build the relevant images
$(DISTRIBUTIONS):
	$(MAKE) -f $(SELF_MAKEFILE) build

build:
ifeq ($(CONTAINER_TOOL),docker)
ifneq ($(findstring $(COMMA),$(PLATFORMS)),)
	@echo "Building Docker image for multiple platforms: $(PLATFORMS)"
	@echo "Note: multi-platform build does not load image into local Docker daemon."
	$(CONTAINER_TOOL) buildx build \
		$(BUILDX_COMMON_ARGS) \
		--platform $(PLATFORMS)
else
	@echo "Building Docker image for single platform: $(PLATFORMS)"
	$(CONTAINER_TOOL) buildx build \
		$(BUILDX_COMMON_ARGS) \
		--platform $(PLATFORMS) \
		--load
endif
else
	DOCKER_BUILDKIT=1 \
		$(CONTAINER_TOOL) build --pull \
		--tag $(IMAGE) \
		--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
		--build-arg BASE_IMAGE="$(BASE_IMAGE)" \
		--build-arg VERSION="$(VERSION)" \
		-f $(DOCKERFILE) \
		$(CURDIR)
endif

push:
ifeq ($(CONTAINER_TOOL),docker)
	$(MAKE) -f $(SELF_MAKEFILE) ensure-buildx-builder
	$(CONTAINER_TOOL) buildx build \
		$(BUILDX_COMMON_ARGS) \
		--builder $(BUILDX_BUILDER) \
		--platform $(PLATFORMS) \
		--provenance=$(BUILDX_PROVENANCE) \
		--sbom=$(BUILDX_SBOM) \
		--push
else
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

build-local:
	$(MAKE) -f $(SELF_MAKEFILE) build PLATFORMS="$(LOCAL_PLATFORM)"

push-multiarch: ensure-buildx-builder
	$(MAKE) -f $(SELF_MAKEFILE) push PLATFORMS="$(MULTIARCH_PLATFORMS)"
