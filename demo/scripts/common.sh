#!/usr/bin/env bash

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

# This scripts invokes `kind build image` so that the resulting
# image has a containerd with CDI support.
#
# Usage: kind-build-image.sh <tag of generated image>

# A reference to the current directory where this script is located
SCRIPTS_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

# The name of the example driver 
: ${DRIVER_NAME:=dra-example-driver}

# The registry, image and tag for the example driver
: ${DRIVER_IMAGE_REGISTRY:="registry.example.com"}
: ${DRIVER_IMAGE_NAME:="${DRIVER_NAME}"}
: ${DRIVER_IMAGE_TAG:="v0.1.0"}
: ${DRIVER_IMAGE_PLATFORM:="ubuntu22.04"}

# The kubernetes tag to build the kind cluster from
# From https://github.com/kubernetes/kubernetes/tags
: ${KIND_K8S_TAG:="v1.28.0"}

# At present, kind has a new enough node image that we don't need to build our
# own. This won't always be true and we may need to set the variable below to
# 'true' from time to time as things change.
: ${BUILD_KIND_IMAGE:="false"}

# The name of the kind cluster to create
: ${KIND_CLUSTER_NAME:="${DRIVER_NAME}-cluster"}

# The path to kind's cluster configuration file
: ${KIND_CLUSTER_CONFIG_PATH:="${SCRIPTS_DIR}/kind-cluster-config.yaml"}

# The derived name of the driver image to build
: ${DRIVER_IMAGE:="${DRIVER_IMAGE_REGISTRY}/${DRIVER_IMAGE_NAME}:${DRIVER_IMAGE_TAG}"}

# The name of the kind image to build / run
: ${KIND_IMAGE:="kindest/node:${KIND_K8S_TAG}"}

