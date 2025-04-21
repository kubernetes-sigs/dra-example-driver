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

# This scripts invokes `kind build image` so that the resulting
# image has a containerd with CDI support.
#
# Usage: kind-build-image.sh <tag of generated image>

# A reference to the current directory where this script is located
CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/common.sh"

${KIND} create cluster \
	--name "${KIND_CLUSTER_NAME}" \
	--image "${KIND_IMAGE}" \
	--config "${KIND_CLUSTER_CONFIG_PATH}" \
	--wait 2m

# udev \
# libudev-dev wget  \
# cmake \
# pkg-config \ 
# git \
# gcc \
# g++ \
# libusb-1.0-0-dev \
# usbutils -y \
docker exec -it "${KIND_CLUSTER_NAME}-worker" bash -c "apt-get update && apt-get install -y udev libudev-dev pkg-config usbutils libusb-1.0-0-dev"

# docker exec -it "${KIND_CLUSTER_NAME}-worker" bash -c "apt-get update && apt-get install -y git gcc g++ wget cmake"
# docker exec -it "${KIND_CLUSTER_NAME}-worker" bash -c "\
# wget https://go.dev/dl/go1.23.7.linux-amd64.tar.gz && \
# tar -C /usr/local -xzf go1.23.7.linux-amd64.tar.gz && \
# export PATH=\$PATH:/usr/local/go/bin && \
# export GOPATH=/go && \
# git clone https://github.com/raspberrypi/pico-sdk /app/picosdk && \
# cd /app/picosdk && git submodule update --init && \
# export PICO_SDK_PATH=/app/picosdk && \
# git clone https://github.com/raspberrypi/picotool && \
# cd picotool && mkdir build && cd build && \
# cmake .. && make &&  make install"
