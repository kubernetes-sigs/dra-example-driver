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
: ${DRIVER_IMAGE_REGISTRY:="registry.k8s.io/dra-example-driver"}
: ${DRIVER_IMAGE_NAME:="${DRIVER_NAME}"}
: ${DRIVER_IMAGE_TAG:="$(cat $(git rev-parse --show-toplevel)/deployments/helm/${DRIVER_NAME}/Chart.yaml | grep appVersion | sed 's/"//g' | sed -n 's/^appVersion: //p')"}
# Use DRIVER_IMAGE_OS as the canonical variable name.
# DRIVER_IMAGE_PLATFORM is a deprecated compatibility fallback.
if [[ -n "${DRIVER_IMAGE_PLATFORM:-}" && -n "${DRIVER_IMAGE_OS:-}" && "${DRIVER_IMAGE_PLATFORM}" != "${DRIVER_IMAGE_OS}" ]]; then
    echo "Both DRIVER_IMAGE_PLATFORM and DRIVER_IMAGE_OS are set with different values."
    echo "Use DRIVER_IMAGE_OS only, or set both to the same value."
    return 1
fi
if [[ -n "${DRIVER_IMAGE_PLATFORM:-}" && -z "${DRIVER_IMAGE_OS:-}" ]]; then
    DRIVER_IMAGE_OS="${DRIVER_IMAGE_PLATFORM}"
fi
: ${DRIVER_IMAGE_OS:="ubuntu22.04"}

# Use PLATFORMS as the canonical variable name.
# DRIVER_IMAGE_PLATFORMS is a deprecated compatibility fallback.
if [[ -n "${PLATFORMS:-}" && -n "${DRIVER_IMAGE_PLATFORMS:-}" ]]; then
    echo "Both PLATFORMS and DRIVER_IMAGE_PLATFORMS are set."
    echo "Use PLATFORMS only. DRIVER_IMAGE_PLATFORMS is deprecated."
    return 1
fi
if [[ -z "${PLATFORMS:-}" && -n "${DRIVER_IMAGE_PLATFORMS:-}" ]]; then
    PLATFORMS="${DRIVER_IMAGE_PLATFORMS}"
fi
: ${PLATFORMS:="linux/amd64,linux/arm64"}

# The kubernetes repo to build the kind cluster from
: ${KIND_K8S_REPO:="https://github.com/kubernetes/kubernetes.git"}

# The kubernetes tag to build the kind cluster from
# From ${KIND_K8S_REPO}/tags
: ${KIND_K8S_TAG:="v1.36.0"}

# The name of the kind cluster to create
: ${KIND_CLUSTER_NAME:="${DRIVER_NAME}-cluster"}

# The path to kind's cluster configuration file
: ${KIND_CLUSTER_CONFIG_PATH:="${SCRIPTS_DIR}/kind-cluster-config.yaml"}

# The derived name of the driver image to build
: ${DRIVER_IMAGE:="${DRIVER_IMAGE_REGISTRY}/${DRIVER_IMAGE_NAME}:${DRIVER_IMAGE_TAG}"}

# The name of the kind image to build / run
: ${KIND_IMAGE:="kindest/node:${KIND_K8S_TAG}"}

# Container tool, e.g. docker/podman
if [[ -z "${CONTAINER_TOOL}" ]]; then
    if [[ -n "$(which docker)" ]]; then
        echo "Docker found in PATH."
        CONTAINER_TOOL=docker
    elif [[ -n "$(which podman)" ]]; then
        echo "Podman found in PATH."
        CONTAINER_TOOL=podman
    else
        echo "No container tool detected. Please install Docker or Podman."
        return 1
    fi
fi

: ${KIND:="env KIND_EXPERIMENTAL_PROVIDER=${CONTAINER_TOOL} kind"}
