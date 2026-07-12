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

# Creates a minikube cluster for the demo with CDI support enabled.
# See demo/scripts/common.sh for configuration (cluster name, etc.).

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/../../scripts/common.sh"

# minikube only honours the last --extra-config flag for a given key. Since
# feature-gates is a single key per component, all gates must be passed as one
# comma-separated value rather than as repeated --extra-config flags for the
# same component.
FEATURE_GATES="DynamicResourceAllocation=true,DRAAdminAccess=true,DRAWorkloadResourceClaims=true,GangScheduling=true,GenericWorkload=true,DRAExtendedResource=true,DRADeviceBindingConditions=true,DRANodeAllocatableResources=true,DRAConsumableCapacity=true"

${MINIKUBE} start \
    --container-runtime=containerd \
    --driver=${MINIKUBE_DRIVER} \
    --kubernetes-version=${KIND_K8S_TAG} \
    --cpus=4 \
    --cni=flannel \
    --extra-config="apiserver.runtime-config=resource.k8s.io/v1beta1=true,scheduling.k8s.io/v1alpha2=true" \
    --extra-config="apiserver.feature-gates=${FEATURE_GATES}" \
    --extra-config="scheduler.feature-gates=${FEATURE_GATES}" \
    --extra-config="controller-manager.feature-gates=${FEATURE_GATES}" \
    --extra-config="kubelet.feature-gates=${FEATURE_GATES}"

# Enable CDI support in containerd and restart it
${MINIKUBE} ssh "sudo sed -i '/\[plugins\.\"io\.containerd\.grpc\.v1\.cri\"]/a\    enable_cdi = true' /etc/containerd/config.toml"
${MINIKUBE} ssh "sudo systemctl restart containerd"

# If a driver image already exists, load it into the cluster
EXISTING_IMAGE_ID="$(${CONTAINER_TOOL} images --filter "reference=${DRIVER_IMAGE}" --quiet)"
if [ "${EXISTING_IMAGE_ID}" != "" ]; then
    "${CURRENT_DIR}/../../scripts/load-driver-image-into-minikube.sh"
fi

set +x
printf '\033[0;32m'
echo "Cluster creation complete: ${MINIKUBE_CLUSTER_NAME}"
printf '\033[0m'
