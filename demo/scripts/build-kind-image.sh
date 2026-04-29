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

# If an image ID already exists for the image we plan to build, we are done.
EXISTING_IMAGE_ID="$(${CONTAINER_TOOL} images --filter "reference=${KIND_IMAGE}" -q)"
if [ "${EXISTING_IMAGE_ID}" != "" ]; then
	exit 0
fi

if [[ "${CONTAINER_TOOL}" != "docker" ]]; then
    echo "Building kind images requires Docker. Cannot use '${CONTAINER_TOOL}'"
    exit 1
fi

# Build the kind base image
${KIND} build node-image --image "${KIND_IMAGE}" --type release "${KIND_K8S_TAG}"
