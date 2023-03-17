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

# If an image ID already exists for the image we plan to build, we are done.
EXISTING_IMAGE_ID="$(docker images --filter "reference=${KIND_IMAGE}" -q)"
if [ "${EXISTING_IMAGE_ID}" != "" ]; then
	exit 0
fi

# Create a temorary directory to hold all the artifacts we need for building the image
TMP_DIR="$(mktemp -d)"
cleanup() {
    rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

# Set some build variables
KIND_K8S_REPO="https://github.com/kubernetes/kubernetes.git "
KIND_K8S_DIR="${TMP_DIR}/kubernetes-${KIND_K8S_TAG}"
KIND_CONTAINERD_DIR="${TMP_DIR}/${KIND_CONTAINERD_TAG}"
KIND_IMAGE_BASE="${KIND_IMAGE}-base"

ARCH="$(uname -m)"
ARCH="${ARCH/x86_64/amd64}"
ARCH="${ARCH/aarch64/arm64}"

# Checkout the version of kubernetes we want to build our kind image from
git clone --depth 1 --branch ${KIND_K8S_TAG} ${KIND_K8S_REPO} ${KIND_K8S_DIR}

# Download the artifacts for the version of containerd we want to install
mkdir -p "${KIND_CONTAINERD_DIR}"
curl -L --silent https://github.com/kind-ci/containerd-nightlies/releases/download/${KIND_CONTAINERD_TAG}/${KIND_CONTAINERD_TAG}-linux-${ARCH}.tar.gz | tar -C "${KIND_CONTAINERD_DIR}" -vzxf -
curl -L --silent https://github.com/kind-ci/containerd-nightlies/releases/download/${KIND_CONTAINERD_TAG}/runc.${ARCH} > "${KIND_CONTAINERD_DIR}/runc"

# Build the kind base image
kind build node-image --image "${KIND_IMAGE_BASE}" "${KIND_K8S_DIR}"

# Build a dockerfile to install the containerd artifacts
# into the build image and update it to enable CDI
cat > "${KIND_CONTAINERD_DIR}/Dockerfile" <<EOF
FROM ${KIND_IMAGE_BASE}

COPY bin/* /usr/local/bin/
RUN chmod a+rx /usr/local/bin/*
COPY runc /usr/local/sbin
RUN chmod a+rx /usr/local/sbin/runc

# Enable CDI as described in https://github.com/container-orchestrated-devices/container-device-interface#containerd-configuration
RUN sed -i -e '/\[plugins."io.containerd.grpc.v1.cri"\]/a \ \ enable_cdi = true' /etc/containerd/config.toml
EOF

# Build the new image, tag it, and remove the kind base image
docker build --tag "${KIND_IMAGE}" "${KIND_CONTAINERD_DIR}"
docker image rm "${KIND_IMAGE_BASE}"
