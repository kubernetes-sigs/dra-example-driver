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

VFIO_GPU_SETUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../vfio-gpu" && pwd)"

: "${VFIO_GPU:=false}"
: "${VFIO_SETUP_HOST:=false}"
: "${FAKE_PCI_DEVICES:=8}"

vfio_gpu_enabled() {
    case "${VFIO_GPU,,}" in
    1 | true | yes | on) return 0 ;;
    esac
    return 1
}

# VFIO_SETUP_HOST lets the demo build and load the synthetic PCI devices in
# demo/vfio-gpu itself. It is off by default because that inserts out-of-tree
# modules into the running kernel, which must not happen as a side effect of
# creating a cluster, and because an unbound host may simply be one whose real
# devices have not been bound to vfio-pci yet.
vfio_setup_host_enabled() {
    case "${VFIO_SETUP_HOST,,}" in
    1 | true | yes | on) return 0 ;;
    esac
    return 1
}

vfio_fatal() {
    echo "ERROR: $*" >&2
    exit 1
}

vfio_preflight() {
    echo "Pre-flight checks"

    if [[ "$(uname -s)" != "Linux" ]]; then
        vfio_fatal "Host is $(uname -s). The vfio-gpu demo needs a Linux host" \
            "kernel with vfio-pci-bound devices."
    fi

    # common.sh already selected a container runtime; confirm it is usable.
    [[ -n "${CONTAINER_TOOL}" ]] \
        || vfio_fatal "CONTAINER_TOOL is empty; source demo/scripts/common.sh first"
    command -v "${CONTAINER_TOOL}" >/dev/null \
        || vfio_fatal "CONTAINER_TOOL=${CONTAINER_TOOL} but '${CONTAINER_TOOL}' not found in PATH"
    export CONTAINER_TOOL

    # kind defaults to docker; the podman backend is opt-in via env.
    if [[ "${CONTAINER_TOOL}" == "podman" ]]; then
        export KIND_EXPERIMENTAL_PROVIDER=podman
        echo "kind backend: podman (KIND_EXPERIMENTAL_PROVIDER=podman)"
    fi

    # common.sh runs kind through `go tool -modfile hack/tools/go.mod`, so Go
    # is the real requirement rather than a kind binary on PATH.
    command -v go >/dev/null \
        || vfio_fatal "go not found in PATH; kind runs via 'go tool' from hack/tools"

    [[ -f "${KIND_CLUSTER_CONFIG_PATH}" ]] \
        || vfio_fatal "kind config not found at ${KIND_CLUSTER_CONFIG_PATH}"

    # Loading the synthetic devices needs root. Prime the credentials here so
    # the prompt cannot interrupt cluster creation half way through.
    if vfio_setup_host_enabled && ! sudo -n true 2>/dev/null; then
        echo "VFIO_SETUP_HOST needs root (you'll be prompted once)"
        sudo -v || vfio_fatal "sudo authentication failed"
    fi
}

# Prints the BDF of every PCI function currently bound to vfio-pci.
vfio_bound_devices() {
    local entry name
    shopt -s nullglob
    for entry in /sys/bus/pci/drivers/vfio-pci/[0-9a-fA-F]*:*; do
        name="${entry##*/}"
        [[ "${name}" =~ ^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9a-fA-F]$ ]] || continue
        echo "${name}"
    done
    shopt -u nullglob
}

# Builds the fake-pci/fake-iommu modules, loads them, and binds the resulting
# devices to vfio-pci.
vfio_setup_host() {
    echo "VFIO_SETUP_HOST is set: creating ${FAKE_PCI_DEVICES} synthetic vfio-pci device(s)"
    # `sudo env VAR=...` rather than `sudo VAR=...`: a strict sudoers env_reset
    # policy can refuse environment assignments passed directly to sudo.
    sudo env FAKE_PCI_DEVICES="${FAKE_PCI_DEVICES}" \
        bash "${VFIO_GPU_SETUP_DIR}/setup-host-vfio-pci.sh" setup \
        || vfio_fatal "synthetic device setup failed; see ${VFIO_GPU_SETUP_DIR}/README.md"
}

# Unloads devices created by vfio_setup_host. Devices bound by the operator are
# left alone, so this only runs when VFIO_SETUP_HOST created them.
vfio_cleanup_host() {
    vfio_gpu_enabled || return 0
    vfio_setup_host_enabled || return 0

    echo "Removing synthetic vfio-pci devices"
    sudo bash "${VFIO_GPU_SETUP_DIR}/setup-host-vfio-pci.sh" cleanup \
        || echo "  cleanup failed; unload fake-pci/fake-iommu manually" >&2
}

verify_vfio_setup() {
    echo "Checking for vfio-pci bindings on the host"

    local devs=()
    mapfile -t devs < <(vfio_bound_devices)

    if [[ ${#devs[@]} -eq 0 ]] && vfio_setup_host_enabled; then
        vfio_setup_host
        mapfile -t devs < <(vfio_bound_devices)
        [[ ${#devs[@]} -gt 0 ]] \
            || vfio_fatal "host setup completed but nothing is bound to vfio-pci"
    fi

    if [[ ${#devs[@]} -eq 0 ]]; then
        echo "No PCI devices bound to vfio-pci on this host."
        echo ""
        echo "Either bind real devices to vfio-pci, or create synthetic ones:"
        echo ""
        echo "    sudo FAKE_PCI_DEVICES=8 bash ${VFIO_GPU_SETUP_DIR}/setup-host-vfio-pci.sh"
        echo ""
        echo "Re-run with VFIO_SETUP_HOST=true to do that automatically:"
        echo ""
        echo "    VFIO_GPU=true VFIO_SETUP_HOST=true ./demo/clusters/kind/create-cluster.sh"
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
