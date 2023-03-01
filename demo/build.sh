#!/usr/bin/env bash

set -e

: ${REGISTRY:=registry.example.com}
: ${IMAGE:=dra-example-driver}
: ${VERSION:=v0.1.0}

: ${BUILDER:=}
: ${PLATFORM:=ubuntu22.04}

: ${TMPDIR:=$(dirname $(mktemp -u))}

if [ "${BUILDER}" == "" ]; then
	if [ "$(uname)" = "Darwin" ]; then
		export BUILDER=podman
	else
		export BUILDER=docker
	fi
fi

if [ "${BUILDER}" != "docker" ] && [ "${BUILDER}" != "podman" ]; then
	echo "BUILDER must be set to either 'docker' or 'podman'"
	exit 1
fi

function maybe-start-podman-vm() {
	local currentvmstate="$(podman machine info --format={{.Host.MachineState}})"
	if [ "${currentvmstate}" == "Running" ]; then
		return
	fi
	local existingvms="$(podman machine info --format={{.Host.NumberOfMachines}})"
	if [ "${existingvms}" -eq "0" ]; then
		podman machine init --cpus 2 --memory 2048 --disk-size 20
	fi
	local firstvm="$(podman machine list --format={{.Name}} | head -n 1)"
	podman machine start ${firstvm%"*"}
}
if [ "${BUILDER}" == "podman" ]; then
	maybe-start-podman-vm
	firstvm="$(podman machine list --format={{.Name}} | head -n 1)"
	export CONTAINER_CONNECTION=${firstvm%"*"}-root
fi

# Go back to the root directory of this repo
cd ..

# Regenerate the CRDs and build the container image
DOCKER=${BUILDER} make docker-vendor
DOCKER=${BUILDER} make docker-generate
DOCKER=${BUILDER} make -f deployments/container/Makefile ${PLATFORM}
${BUILDER} image save ${REGISTRY}/${IMAGE}:${VERSION} > ${TMPDIR}/${IMAGE}.tgz

# Load the new image into minikube if it is running
minikube status --profile=${IMAGE} > /dev/null 2>&1
if [ "${?}" == "0" ]; then
	minikube image load \
		--profile=${IMAGE} \
		--overwrite \
		${TMPDIR}/${IMAGE}.tgz
fi
