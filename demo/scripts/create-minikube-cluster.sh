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

# 该脚本启动或创建一个 minikube 集群（profile），启用 DynamicResourceAllocation，
# 并通过 containerd 的配置开启 CDI 支持。

set -ex
set -o pipefail

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"
source "${CURRENT_DIR}/common.sh"

# 如果已存在同名 profile，则跳过创建
if minikube status --profile="${MINIKUBE_PROFILE_NAME}" &>/dev/null; then
  echo "Minikube cluster (profile: ${MINIKUBE_PROFILE_NAME}) already exists. Skip creation."
  exit 0
fi

# 如需指定 k8s 版本，可加: --kubernetes-version "v1.32.0"
minikube start \
  --profile="${MINIKUBE_PROFILE_NAME}" \
  --driver=docker \
  --container-runtime=containerd \
  --feature-gates=DynamicResourceAllocation=true \
  --extra-config=apiserver.runtime-config=resource.k8s.io/v1beta1=true \
  --extra-config=apiserver.v=1 \
  --extra-config=controller-manager.v=1 \
  --extra-config=scheduler.v=1 \
  --extra-config=kubelet.v=1 \
  --extra-config=containerd.plugins.\"io.containerd.grpc.v1.cri\".enable_cdi=true \
  --wait=all

# 也可以用 --wait=apiserver,system_pods 等等按需配置
# 如果想改用别的 driver(virtualbox/hyperv/podman等)，修改 --driver 即可
