#!/usr/bin/env bash
# minikube start \
#   --driver=docker \
#   --kubernetes-version=v1.32.0 \
#   --feature-gates=DynamicResourceAllocation=true \
#   --extra-config=apiserver.runtime-config=resource.k8s.io/v1beta1=true \
#   --extra-config=apiserver.v=1 \
#   --extra-config=scheduler.v=1 \
#   --extra-config=controller-manager.v=1 \
#   --extra-config=kubelet.v=1

minikube start \
  --driver=docker \
  --kubernetes-version=v1.32.0 \
  --feature-gates=DynamicResourceAllocation=true \
  --extra-config=apiserver.runtime-config=resource.k8s.io/v1beta1=true \
  --extra-config=apiserver.v=1 \
  --extra-config=scheduler.v=1 \
  --extra-config=controller-manager.v=1 \
  --extra-config=kubelet.v=1 \
  --gpus all \
  --mount-string="/usr/bin/nvidia-smi:/usr/bin/nvidia-smi" \
  --mount-string="/usr/bin/nvidia-ctk:/usr/bin/nvidia-ctk" \
  --mount-string="/run/nvidia-fabricmanager/socket:/run/nvidia-fabricmanager/socket" \
  --mount-string="/dev:/dev" \
  --mount
