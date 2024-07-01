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
DOCKER ?= docker
MKDIR ?= mkdir
DISTRIBUTIONS := ubuntu22.04 ubi9
DOCKERFILE := $(CURDIR)/deployments/container/Dockerfile

include $(CURDIR)/common.mk

IMAGE_TAG := $(VERSION)
IMAGE = $(IMAGE_NAME):$(IMAGE_TAG)

##### Public rules #####
.PHONY: $(DISTRIBUTIONS)

ubuntu22.04: BASE_IMAGE = docker.io/ubuntu:22.04

ubi9: BASE_IMAGE = registry.access.redhat.com/ubi9/ubi-micro:latest

# Use a generic build target to build the relevant imsages
$(DISTRIBUTIONS):
	DOCKER_BUILDKIT=1 \
		$(DOCKER) build --pull \
		--tag $(IMAGE) \
		--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
		--build-arg BASE_IMAGE="$(BASE_IMAGE)" \
		--build-arg VERSION="$(VERSION)" \
		-f $(DOCKERFILE) \
		$(CURDIR)
