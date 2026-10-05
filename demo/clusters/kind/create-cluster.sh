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
#
# Optional vfio-gpu mode (off by default): set VFIO_GPU=true to create a cluster
# with host PCI sysfs and /dev/vfio bind-mounted into nodes. Requires a Linux
# host with devices already bound to vfio-pci. See demo/clusters/kind/README.md.

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/../../scripts/common.sh"
source "${CURRENT_DIR}/../../scripts/vfio-kind.sh"

: "${VFIO_GPU:=false}"
: "${VFIO_KIND_CLUSTER_CONFIG_PATH:=${CURRENT_DIR}/kind-cluster-config-vfio.yaml}"

if vfio_gpu_enabled; then
    vfio_preflight
    verify_vfio_setup
    KIND_CLUSTER_CONFIG_PATH="${VFIO_KIND_CLUSTER_CONFIG_PATH}"
    KIND_CONFIG="${KIND_CLUSTER_CONFIG_PATH}"

    echo "Pre-pulling kind node image for vfio-gpu: ${KIND_IMAGE}"
    "${CONTAINER_TOOL}" pull -q "${KIND_IMAGE}" >/dev/null \
        || echo "Could not pre-pull ${KIND_IMAGE}; kind will pull on create"
else
    # Build the kind image and create a test cluster
    if ! "${CONTAINER_TOOL}" manifest inspect "${KIND_IMAGE}"; then
        ${SCRIPTS_DIR}/build-kind-image.sh
    fi
fi

${KIND} create cluster \
    --name "${KIND_CLUSTER_NAME}" \
    --image "${KIND_IMAGE}" \
    --config "${KIND_CLUSTER_CONFIG_PATH}" \
    --wait 2m

if vfio_gpu_enabled; then
    vfio_post_create_config
fi

# If a driver image already exists load it into the cluster
EXISTING_IMAGE_ID="$(${CONTAINER_TOOL} images --filter "reference=${DRIVER_IMAGE}" -q)"
if [ "${EXISTING_IMAGE_ID}" != "" ]; then
    ${SCRIPTS_DIR}/load-driver-image-into-kind.sh
fi

set +x
printf '\033[0;32m'
echo "Cluster creation complete: ${KIND_CLUSTER_NAME}"
if vfio_gpu_enabled; then
    echo "VFIO_GPU mode: install the driver with --set deviceProfile=vfio-gpu and apply demo/clusters/kind/vfio-gpu-test.yaml"
fi
printf '\033[0m'
