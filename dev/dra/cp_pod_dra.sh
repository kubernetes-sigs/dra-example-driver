#!/usr/bin/env bash
#
# 功能：
#   1. 自动检测指定 Namespace 下唯一的 Pod 名称（假设该 Namespace 下只有 1 个 Pod）。
#   2. 将本地可执行文件复制到该 Pod 的 /root 目录下，并赋予执行权限。
#   3. 切换到正确的 K8s 集群上下文后再执行脚本。
#
# 使用示例：
#   ./copy-file-to-single-pod.sh [可执行文件路径] [Namespace]
#
# 参数说明：
#   1. 可执行文件路径（可选），默认为 ./executable1
#   2. Namespace（可选），默认为 dra-example-driver
#
# 注意：
#   - 如果目标 Namespace 下有多个 Pod，本脚本只会取列表中第一个 Pod。
#   - 如果没有任何 Pod，则会提示错误并退出。
#   - 请确保脚本执行者对目标 Namespace 有足够的访问权限。

# 默认变量
EXEC_FILE_1=${1:-"./executable1"}
NAMESPACE=${2:-"dra-example-driver"}
CONTAINER_ROOT="/root"

# 打印提示信息
echo "=== 准备在 Namespace: $NAMESPACE 下获取唯一 Pod，并将文件复制到其 $CONTAINER_ROOT 目录 ==="
echo "本地文件: $EXEC_FILE_1"
echo "--------------------------------------------------------------------------------------"

# 1. 获取指定 Namespace 下唯一 Pod 名称
POD_NAME=$(kubectl get pods -n "$NAMESPACE" --no-headers -o custom-columns=":metadata.name" | head -n 1)

if [ -z "$POD_NAME" ]; then
  echo "错误：在 Namespace $NAMESPACE 下没有找到任何 Pod，请检查后重试。"
  exit 1
fi

echo "检测到的 Pod: $POD_NAME"

# 2. 拷贝文件到 Pod
echo "=== 开始将本地文件拷贝到 Pod /root 目录中... ==="
kubectl cp "$EXEC_FILE_1" "$NAMESPACE/$POD_NAME:$CONTAINER_ROOT"
if [ $? -ne 0 ]; then
  echo "错误：拷贝文件 $EXEC_FILE_1 到 $POD_NAME 失败，请检查文件路径和 Pod 状态。"
  exit 1
fi

# 3. 查看容器 /root 目录下的文件情况
echo "=== 拷贝完成，开始查看容器内 /root 目录下的文件情况 ==="
kubectl exec -n "$NAMESPACE" "$POD_NAME" -- ls -l /root

# 4. 赋予执行权限
echo "=== 开始赋予执行权限... ==="
kubectl exec -n "$NAMESPACE" "$POD_NAME" -- chmod +x /root/$(basename "$EXEC_FILE_1")

# 结束
echo "=== 脚本执行完毕 ==="

