#!/usr/bin/env bash

# Copyright The Kubernetes Authors.
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

# Creates a kind cluster for the demo (optionally from a custom node image with
# containerd CDI support). See demo/scripts/common.sh for configuration.

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/../../scripts/common.sh"

# Build the kind image and create a test cluster
if ! "${CONTAINER_TOOL}" manifest inspect "${KIND_IMAGE}"; then
	${SCRIPTS_DIR}/build-kind-image.sh
fi

${KIND} create cluster \
	--name "${KIND_CLUSTER_NAME}" \
	--image "${KIND_IMAGE}" \
	--config "${KIND_CLUSTER_CONFIG_PATH}" \
	--wait 2m

# If a driver image already exists load it into the cluster
EXISTING_IMAGE_ID="$(${CONTAINER_TOOL} images --filter "reference=${DRIVER_IMAGE}" -q)"
if [ "${EXISTING_IMAGE_ID}" != "" ]; then
	${SCRIPTS_DIR}/load-driver-image-into-kind.sh
fi

set +x
printf '\033[0;32m'
echo "Cluster creation complete: ${KIND_CLUSTER_NAME}"
printf '\033[0m'
