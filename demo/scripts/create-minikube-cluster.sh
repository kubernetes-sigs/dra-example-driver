#!/usr/bin/env bash

# Copyright 2026 The Kubernetes Authors.
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

# This script deploys a minikube cluster and modifies its configuration
# so the resulting cluster has cdi support
#
# Usage: create-minikube-cluster.sh

# A reference to the current directory where this script is located
CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/common.sh"


${MINIKUBE} start \
    --container-runtime=containerd \
    --driver=docker \
    --extra-config=apiserver.runtime-config=resource.k8s.io/v1beta1=true

# This adds enable_cdi to config.toml and restarts containerd
${MINIKUBE} ssh "sudo sed -i '/\[plugins\.\"io\.containerd\.grpc\.v1\.cri\"]/a\    enable_cdi = true' /etc/containerd/config.toml"
${MINIKUBE} ssh "sudo systemctl restart containerd"
