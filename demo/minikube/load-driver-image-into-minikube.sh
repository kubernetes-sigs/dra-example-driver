#!/usr/bin/env bash

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/common.sh"

# Work around kind not loading image with podman
make -f deployments/container/Makefile "ubuntu22.04"      
IMAGE_ARCHIVE=driver_image.tar
docker tag registry.example.com/rasberrypi-pico-driver:v0.1.0 registry.k8s.io/rasberrypi-pico-driver:v0.1.0 
${CONTAINER_TOOL} save -o "${IMAGE_ARCHIVE}" "${DRIVER_IMAGE}" && \
minikube image load "${IMAGE_ARCHIVE}"
# ${KIND} load image-archive \
# 	--name "${KIND_CLUSTER_NAME}" \
# 	"${IMAGE_ARCHIVE}"
rm "${IMAGE_ARCHIVE}"