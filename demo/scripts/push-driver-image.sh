#!/usr/bin/env bash

# Copyright 2025 The Kubernetes Authors.
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

# A reference to the current directory where this script is located
CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail


source "${CURRENT_DIR}/common.sh"
check_demo_config || exit 1

cd "${CURRENT_DIR}/../.."

# Set build variables
: "${PLATFORMS:=linux/amd64,linux/arm64,linux/ppc64le}"
export REGISTRY="${DRIVER_IMAGE_REGISTRY}"
export IMAGE="${DRIVER_IMAGE_NAME}"
export VERSION="${DRIVER_IMAGE_TAG}"
export PLATFORMS
export CONTAINER_TOOL="${CONTAINER_TOOL}"

# Regenerate CRDs/deepcopy in the repo's devel container (root Makefile docker-% target).
make docker-generate

make -f deployments/container/Makefile push
