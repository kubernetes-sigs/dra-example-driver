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

# 本脚本用于构建 DRA driver 镜像，输出到本地容器环境中。
# 最终镜像名: ${DRIVER_IMAGE} (含tag)

set -ex
set -o pipefail

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"
source "${CURRENT_DIR}/common.sh"

# 创建临时目录存放中间生成物
TMP_DIR="$(mktemp -d)"
cleanup() {
    rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

# Go back to the repo root
cd "${CURRENT_DIR}/../.."

# 设置构建相关变量
export REGISTRY="${DRIVER_IMAGE_REGISTRY}"
export IMAGE="${DRIVER_IMAGE_NAME}"
export VERSION="${DRIVER_IMAGE_TAG}"
export CONTAINER_TOOL="${CONTAINER_TOOL}"

# 调用 makefile 构建
make docker-generate
make -f deployments/container/Makefile "${DRIVER_IMAGE_PLATFORM}"
