#!/usr/bin/env bash

echo "=== 正在查找并杀死所有 npc、dlv、kube-scheduler 相关进程 ==="
PATTERN="npc|dlv|kube-scheduler"

# 查找所有匹配到的进程并排除 grep 本身
PIDS=$(ps -ef | grep -E "$PATTERN" | grep -v grep | awk '{print $2}')

if [ -z "$PIDS" ]; then
  echo "未找到任何 npc、dlv 或 kube-scheduler 相关进程。"
  exit 0
fi

echo "即将终止以下进程："
# 再查看一遍详细信息
ps -ef | grep -E "$PATTERN" | grep -v grep

# 执行 kill -9
kill -9 $PIDS
echo "进程已全部结束。"

