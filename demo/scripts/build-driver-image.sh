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
CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/common.sh"

# Create a temorary directory to hold all the artifacts we need for building the image
TMP_DIR="$(mktemp -d)"
cleanup() {
    rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

# Go back to the root directory of this repo
cd ${CURRENT_DIR}/../..

# Set build variables
export REGISTRY="${DRIVER_IMAGE_REGISTRY}"
export IMAGE="${DRIVER_IMAGE_NAME}"
export VERSION="${DRIVER_IMAGE_TAG}"
export CONTAINER_TOOL="${CONTAINER_TOOL}"

# Regenerate the CRDs and build the container image
make docker-generate

# When SKIP_LOCAL_BUILD_FOR_DOCKER_MULTIARCH=1 (set only by push-release-artifacts for
# Docker multi-arch), skip the local image build: push-driver-image performs one
# buildx --push for all platforms. Unset or any value other than 1 runs the normal
# local build below (including explicitly setting the variable to 0).
if [[ "${SKIP_LOCAL_BUILD_FOR_DOCKER_MULTIARCH:-}" == "1" && "${CONTAINER_TOOL}" == "docker" && "${PLATFORMS}" == *,* ]]; then
    exit 0
fi

# For single-arch (docker or non-docker), build the image locally before push.
make -f deployments/container/Makefile "${DRIVER_IMAGE_OS}"
