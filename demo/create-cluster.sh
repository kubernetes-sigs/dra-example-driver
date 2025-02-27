// C:\Users\loopsaaage\workspace\kind\dlv\ascend-dra-driver\demo\create-cluster.sh
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

# 该脚本用于创建/启动一个基于 minikube 的集群，
# 并将本地已有的 driver 镜像加载到集群中（如果检测到的话）。

set -ex
set -o pipefail

# 取得当前脚本所在目录
CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

source "${CURRENT_DIR}/scripts/common.sh"

# 如果之前有 BUILD_KIND_IMAGE 需求，这里提示已切换到 minikube，不再构建 kind 镜像
if [ "${BUILD_KIND_IMAGE}" = "true" ]; then
  echo "WARNING: BUILD_KIND_IMAGE=true，但已切换至使用 minikube，此步骤将被跳过。"
fi

# 创建或启动 minikube 集群（幂等）
${SCRIPTS_DIR}/create-minikube-cluster.sh

# 如果本地已经存在 DRIVER_IMAGE，则加载到 minikube 集群
EXISTING_IMAGE_ID="$(${CONTAINER_TOOL} images --filter "reference=${DRIVER_IMAGE}" -q)"
if [ "${EXISTING_IMAGE_ID}" != "" ]; then
  # minikube >= v1.25.0 开始支持 image load 命令
  minikube image load "${DRIVER_IMAGE}" --profile="${MINIKUBE_PROFILE_NAME}"
fi

set +x
printf '\033[0;32m'
echo "Cluster creation complete: ${MINIKUBE_PROFILE_NAME}"
printf '\033[0m'
