#!/usr/bin/env bash

NAMESPACE=${1:-dra-example-driver}

# 获取 Namespace 下的第一个 Pod（若有多个，则仅取第一个）
POD_NAME=$(kubectl get pods -n "$NAMESPACE" --no-headers -o custom-columns=":metadata.name" | head -n 1)

# 检查是否获取到 Pod
if [ -z "$POD_NAME" ]; then
  echo "错误：在 Namespace $NAMESPACE 下未找到任何 Pod，无法执行 bash。"
  exit 1
fi

echo "即将在 Pod: $POD_NAME 中执行交互式 bash..."
kubectl exec -it "$POD_NAME" -n "$NAMESPACE" -- bash

