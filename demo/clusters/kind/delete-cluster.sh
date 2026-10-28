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

# Deletes the kind cluster used by the demo. See demo/scripts/common.sh for
# configuration (cluster name, etc.).
#
# When the cluster was created with VFIO_GPU=true VFIO_SETUP_HOST=true, pass
# the same variables here to also unload the synthetic PCI devices.

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/../../scripts/common.sh"
source "${CURRENT_DIR}/../../scripts/vfio-kind.sh"

${KIND} delete cluster \
	--name "${KIND_CLUSTER_NAME}"

vfio_cleanup_host

set +x
printf '\033[0;32m'
echo "Cluster deletion complete: ${KIND_CLUSTER_NAME}"
printf '\033[0m'
