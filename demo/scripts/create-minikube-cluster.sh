#!/usr/bin/env bash

set -ex
set -o pipefail

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"
source "${CURRENT_DIR}/common.sh"

# 检查 minikube 是否已启动，若已存在则跳过创建
if minikube status --profile="${MINIKUBE_PROFILE_NAME}" &>/dev/null; then
  echo "Minikube cluster (profile: ${MINIKUBE_PROFILE_NAME}) already exists. Skip creation."
  exit 0
fi

# **创建临时目录用于存放 containerd 配置**
TMP_CONFIG_DIR="$(mktemp -d)"
cleanup() {
    rm -rf "${TMP_CONFIG_DIR}"
}
trap cleanup EXIT

CONTAINERD_CONFIG="${TMP_CONFIG_DIR}/config.toml"

# **生成新的 containerd 配置，启用 CDI**
cat <<EOF > "${CONTAINERD_CONFIG}"
[plugins."io.containerd.grpc.v1.cri"]
  enable_cdi = true
EOF

# **启动 minikube 并挂载新的 containerd 配置**
minikube start \
  --profile="${MINIKUBE_PROFILE_NAME}" \
  --driver=docker \
  --container-runtime=containerd \
  --feature-gates=DynamicResourceAllocation=true \
  --extra-config=apiserver.runtime-config=resource.k8s.io/v1beta1=true \
  --extra-config=apiserver.v=1 \
  --extra-config=controller-manager.v=1 \
  --extra-config=scheduler.v=1 \
  --extra-config=kubelet.v=1 \
  --mount --mount-string="${CONTAINERD_CONFIG}:/etc/containerd/config.toml" \
  --wait=all

# **重启 containerd 使配置生效**
minikube ssh --profile="${MINIKUBE_PROFILE_NAME}" "sudo systemctl restart containerd"
