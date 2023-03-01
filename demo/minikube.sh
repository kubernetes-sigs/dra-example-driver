#!/usr/bin/env bash

set -e

: ${REGISTRY:=registry.example.com}
: ${IMAGE:=dra-example-driver}
: ${VERSION:=v0.1.0}

: ${DRIVER:=}
: ${RUNTIME:=crio}
: ${NUM_NODES:=1}

: ${TMPDIR:=$(dirname $(mktemp -u))}

export REGISTRY
export IMAGE
export VERSION

if [ "${DRIVER}" == "" ]; then
	if [ "$(uname)" = "Darwin" ]; then
		export DRIVER=podman
	else
		export DRIVER=docker
	fi
fi

if [ "${DRIVER}" != "docker" ] && [ "${DRIVER}" != "podman" ]; then
	echo "BUILDER must be set to either 'docker' or 'podman'"
	exit 1
fi

if [ "${DRIVER}" == "podman" ]; then
	currentvmstate="$(podman machine info --format={{.Host.MachineState}})"
	if [ "${currentvmstate}" != "Running" ]; then
		echo "No podman VM running -- run build.sh and try again"
	fi
	firstvm="$(podman machine list --format={{.Name}} | head -n 1)"
	export CONTAINER_CONNECTION=${firstvm%"*"}-root
fi

minikube delete \
	--profile=${IMAGE}

minikube start \
	--profile=${IMAGE} \
	--driver=${DRIVER} \
	--nodes=${NUM_NODES} \
	--container-runtime=${RUNTIME} \
	--extra-config=apiserver.runtime-config=resource.k8s.io/v1alpha1 \
	--feature-gates=DynamicResourceAllocation=true

for node in $(minikube node list --profile=${IMAGE} | cut -d$'\t' -f 1); do
	minikube ssh \
		--node ${node} \
		--profile=${IMAGE} \
		sudo mkdir /etc/cdi
done

minikube image load \
	--profile=${IMAGE} \
	--overwrite \
	${TMPDIR}/${IMAGE}.tgz
