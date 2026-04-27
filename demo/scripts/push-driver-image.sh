#!/usr/bin/env bash

# Copyright 2025 The Kubernetes Authors.
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

# A reference to the current directory where this script is located
CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail


source "${CURRENT_DIR}/common.sh"
## check return value is 1 then exit
if [ $? -eq 1 ]; then
    echo "Failed to source common.sh"
    exit 1
fi


# Set build variables
export REGISTRY="${DRIVER_IMAGE_REGISTRY}"
export IMAGE="${DRIVER_IMAGE_NAME}"
export VERSION="${DRIVER_IMAGE_TAG}"
export PLATFORMS="${PLATFORMS}"
export CONTAINER_TOOL="${CONTAINER_TOOL}"

make -f deployments/container/Makefile push
