#!/usr/bin/env bash

# Copyright The Kubernetes Authors.
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

# Shared helpers for kind clusters that expose host vfio-pci devices to nodes.
# Sourced by demo/clusters/kind/create-cluster.sh when VFIO_GPU is enabled.

: "${UPSTREAM_HOST_SETUP_HINT:=https://github.com/kubevirt/kubevirt/tree/main/kubevirtci/cluster-up/cluster/kind-1.35-vfio-gpu}"

vfio_gpu_enabled() {
    case "${VFIO_GPU,,}" in
    1 | true | yes | on) return 0 ;;
    esac
    return 1
}

vfio_preflight() {
    echo "Pre-flight checks"

    if [[ "$(uname -s)" != "Linux" ]]; then
        echo "Host is $(uname -s). The vfio-gpu demo needs a Linux host" \
            "kernel with vfio-pci-bound devices."
        exit 1
    fi

    # Auto-detect container runtime if not explicitly set. Prefer docker
    # if both are installed, since it's the kind default (no extra env
    # plumbing required); fall back to podman.
    if [[ -z "${CONTAINER_TOOL}" ]]; then
        if command -v docker >/dev/null; then
            CONTAINER_TOOL=docker
        elif command -v podman >/dev/null; then
            CONTAINER_TOOL=podman
        else
            echo "neither docker nor podman found in PATH"
            exit 1
        fi
        echo "detected CONTAINER_TOOL=${CONTAINER_TOOL}"
    else
        command -v "${CONTAINER_TOOL}" >/dev/null \
            || echo "CONTAINER_TOOL=${CONTAINER_TOOL} but '${CONTAINER_TOOL}' not found in PATH" \
            exit 1
    fi
    export CONTAINER_TOOL

    # kind defaults to docker; the podman backend is opt-in via env.
    if [[ "${CONTAINER_TOOL}" == "podman" ]]; then
        export KIND_EXPERIMENTAL_PROVIDER=podman
        echo "kind backend: podman (KIND_EXPERIMENTAL_PROVIDER=podman)"
    fi

    local tool
    for tool in kind kubectl helm curl sudo; do
        command -v "${tool}" >/dev/null || echo "${tool} not found in PATH" \
            exit 1
    done

    [[ -f "${KIND_CONFIG}" ]] || echo "kind config not found at ${KIND_CONFIG}" \
        exit 1

    if ! sudo -n true 2>/dev/null; then
        echo "Priming sudo credentials (you'll be prompted once)"
        sudo -v || echo "sudo authentication failed" \
            exit 1
    fi
}

verify_vfio_setup() {
    echo "Checking for vfio-pci bindings on the host"

    local devs=()
    local entry name
    shopt -s nullglob
    for entry in /sys/bus/pci/drivers/vfio-pci/[0-9a-fA-F]*:*; do
        name="${entry##*/}"
        [[ "${name}" =~ ^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9a-fA-F]$ ]] || continue
        devs+=("${name}")
    done
    shopt -u nullglob

    if [[ ${#devs[@]} -eq 0 ]]; then
        echo "No PCI devices bound to vfio-pci on this host."
        echo ""
        echo "Prepare the host first, then re-run with VFIO_GPU enabled."
        echo ""
        echo "Synthetic PCI devices (recommended for testing) are provided"
        echo "by kubevirt's kubevirtci kind-1.35-vfio-gpu provider:"
        echo ""
        echo "    ${UPSTREAM_HOST_SETUP_HINT}"
        echo ""
        echo "Get a copy of that directory and run:"
        echo ""
        echo "    sudo FAKE_PCI_DEVICES=8 bash setup-host-vfio-pci.sh"
        echo ""
        echo "Then verify:"
        echo ""
        echo "    ls /sys/bus/pci/drivers/vfio-pci/   # should list BDFs"
        exit 1
    fi

    echo "  found ${#devs[@]} vfio-pci device(s):"
    printf '    %s\n' "${devs[@]}"
}

vfio_post_create_config() {
    echo "Configuring nodes for vfio-gpu (sysfs rw + /dev/vfio/vfio perms)"

    local node
    while IFS= read -r node; do
        [[ -z "${node}" ]] && continue
        echo "  ${node}"
        "${CONTAINER_TOOL}" exec "${node}" mount -o remount,rw /sys

        if "${CONTAINER_TOOL}" exec "${node}" test -e /dev/vfio/vfio; then
            "${CONTAINER_TOOL}" exec "${node}" chmod 666 /dev/vfio/vfio
        else
            echo "    /dev/vfio/vfio not present in ${node}" \
                "- check that vfio-pci is loaded on the host"
            exit 1
        fi

        local discovered
        discovered=$("${CONTAINER_TOOL}" exec "${node}" \
            sh -c 'ls /sys/bus/pci/drivers/vfio-pci/ 2>/dev/null' \
            | grep -E '^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9a-fA-F]$' \
            || true)
        if [[ -n "${discovered}" ]]; then
            echo "${discovered}" | sed 's/^/      /'
        else
            echo "    no vfio-pci devices visible from inside ${node}"
        fi
    done < <(${KIND} get nodes --name "${KIND_CLUSTER_NAME}")
}
