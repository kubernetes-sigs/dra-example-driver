#!/usr/bin/env bash
#
# 说明：
#   1. 请确保本机已经安装并正确配置了 Docker，并且容器名称无误。
#   2. 需要传入本地文件以及目标容器名称，不指定时使用默认变量。
#   3. 拷贝完成后，会使用 docker exec 检查容器 /root 目录下的文件，并可选地赋予可执行权限。

# 设置默认变量（可根据需要修改）
CONTAINER_NAME="dra-example-driver-cluster"
EXEC_FILE_1=${1:-"./executable1"}
CONTAINER_ROOT="/root"

echo "=== 开始拷贝文件到 Docker 容器: $CONTAINER_NAME 的 $CONTAINER_ROOT 目录 ==="
echo "本地文件1: $EXEC_FILE_1"
echo "--------------------------------------------------------------------------------------"

# 拷贝文件到容器
docker cp "$EXEC_FILE_1" "$CONTAINER_NAME:$CONTAINER_ROOT"
if [ $? -ne 0 ]; then
  echo "错误：拷贝 $EXEC_FILE_1 失败，请检查文件名、路径及容器状态！"
  exit 1
fi

echo "=== 拷贝完成，开始查看容器内 $CONTAINER_ROOT 目录下的文件情况 ==="
docker exec "$CONTAINER_NAME" ls -l "$CONTAINER_ROOT"

# 如果需要赋予执行权限，可以加上下面这行
docker exec "$CONTAINER_NAME" chmod +x "$CONTAINER_ROOT/$(basename "$EXEC_FILE_1")"

echo "=== 脚本执行完毕 ==="

