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

# **启动 minikube**
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
  --mount \
  --mount-string="/usr/local/Ascend/driver:/usr/local/Ascend/driver" \
  --wait=all

# **修改 containerd 配置**
minikube ssh --profile="${MINIKUBE_PROFILE_NAME}" <<EOF
  sudo sed -i -r 's|^( *)sandbox_image = .*$|\1sandbox_image = "registry.k8s.io/pause:3.10"|' /etc/containerd/config.toml
  echo '[plugins."io.containerd.grpc.v1.cri"]' | sudo tee -a /etc/containerd/config.toml
  echo '  enable_cdi = true' | sudo tee -a /etc/containerd/config.toml
  sudo systemctl restart containerd
EOF

echo "Minikube cluster (${MINIKUBE_PROFILE_NAME}) is ready!"
