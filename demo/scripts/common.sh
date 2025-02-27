// C:\Users\loopsaaage\workspace\kind\dlv\ascend-dra-driver\demo\scripts\common.sh
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

# 通用环境变量及函数，用于 driver 镜像、minikube profile 名等。

# 脚本目录
SCRIPTS_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

# Example driver 名称
: ${DRIVER_NAME:="dra-example-driver"}

# driver 镜像相关
: ${DRIVER_IMAGE_REGISTRY:="registry.example.com"}
: ${DRIVER_IMAGE_NAME:="${DRIVER_NAME}"}
: ${DRIVER_IMAGE_TAG:="v0.1.0"}
: ${DRIVER_IMAGE_PLATFORM:="ubuntu22.04"}

# 集群名称（minikube 的 profile 名）
: ${MINIKUBE_PROFILE_NAME:="${DRIVER_NAME}-cluster"}

# driver 镜像全称
: ${DRIVER_IMAGE:="${DRIVER_IMAGE_REGISTRY}/${DRIVER_IMAGE_NAME}:${DRIVER_IMAGE_TAG}"}

# 是否曾经用于构建 kind 镜像的标记，暂时保留
: ${BUILD_KIND_IMAGE:="false"}

# 自动检测本机容器工具 docker/podman
if [[ -z "${CONTAINER_TOOL}" ]]; then
    if [[ -n "$(which docker)" ]]; then
        echo "Docker found in PATH."
        CONTAINER_TOOL=docker
    elif [[ -n "$(which podman)" ]]; then
        echo "Podman found in PATH."
        CONTAINER_TOOL=podman
    else
        echo "No container tool detected. Please install Docker or Podman."
        return 1
    fi
fi
