#!/usr/bin/env bash

# Copyright 2024 The Kubernetes Authors.
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

# Very Simple Script for testing the demo

set -e

kind get clusters
kubectl get nodes
kubectl wait --for=condition=Ready nodes/dra-example-driver-cluster-worker --timeout=120s

# Even after verifying that the Pod is Ready and the expected Endpoints resource
# exists with the Pod's IP, the webhook still seems to have "connection refused"
# issues, so retry here until we can ensure it's available before the real tests
# start.
function verify-webhook {
  echo "Waiting for webhook to be available"
  while ! kubectl create --dry-run=server -f- <<-'EOF'
    apiVersion: resource.k8s.io/v1beta1
    kind: ResourceClaim
    metadata:
      name: webhook-test
    spec:
      devices:
        requests:
        - name: gpu
          deviceClassName: gpu.example.com
EOF
  do
    sleep 1
    echo "Retrying webhook"
  done
  echo "Webhook is available"
}
export -f verify-webhook
timeout --foreground 15s bash -c verify-webhook

kubectl create -f demo/gpu-test1.yaml
kubectl create -f demo/gpu-test2.yaml
kubectl create -f demo/gpu-test3.yaml
kubectl create -f demo/gpu-test4.yaml
kubectl create -f demo/gpu-test5.yaml
kubectl create -f demo/gpu-test6.yaml

function gpus-from-logs {
  local logs="$1"
  echo "$logs" | sed -nE "s/^declare -x GPU_DEVICE_[[:digit:]]+=\"(.+)\"$/\1/p"
}

function gpu-id {
  local gpu="$1"
  echo "$gpu" | sed -nE "s/^gpu-([[:digit:]]+)$/\1/p"
}

function gpu-sharing-strategy-from-logs {
  local logs="$1"
  local id="$2"
  echo "$logs" | sed -nE "s/^declare -x GPU_DEVICE_${id}_SHARING_STRATEGY=\"(.+)\"$/\1/p"
}

function gpu-timeslice-interval-from-logs {
  local logs="$1"
  local id="$2"
  echo "$logs" | sed -nE "s/^declare -x GPU_DEVICE_${id}_TIMESLICE_INTERVAL=\"(.+)\"$/\1/p"
}

function gpu-partition-count-from-logs {
  local logs="$1"
  local id="$2"
  echo "$logs" | sed -nE "s/^declare -x GPU_DEVICE_${id}_PARTITION_COUNT=\"(.+)\"$/\1/p"
}

declare -a observed_gpus
function gpu-already-seen {
  local gpu="$1"
  for seen in "${observed_gpus[@]}"; do
    if [[ "$gpu" == "$seen" ]]; then return 0; fi;
  done
  return 1
}

kubectl wait --for=condition=Ready -n gpu-test1 pod/pod0 --timeout=120s
kubectl wait --for=condition=Ready -n gpu-test1 pod/pod1 --timeout=120s
gpu_test_1=$(kubectl get pods -n gpu-test1 | grep -c 'Running')
if [ $gpu_test_1 != 2 ]; then
    echo "gpu_test_1 $gpu_test_1 failed to match against 2 expected pods"
    exit 1
fi

gpu_test1_pod0_ctr0_logs=$(kubectl logs -n gpu-test1 pod0 -c ctr0)
gpu_test1_pod0_ctr0_gpus=$(gpus-from-logs "$gpu_test1_pod0_ctr0_logs")
gpu_test1_pod0_ctr0_gpus_count=$(echo "$gpu_test1_pod0_ctr0_gpus" | wc -w)
if [[ $gpu_test1_pod0_ctr0_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test1/pod0, container ctr0 to have 1 GPU, but got $gpu_test1_pod0_ctr0_gpus_count: $gpu_test1_pod0_ctr0_gpus"
  exit 1
fi
gpu_test1_pod0_ctr0_gpu="$gpu_test1_pod0_ctr0_gpus"
if gpu-already-seen "$gpu_test1_pod0_ctr0_gpu"; then
  echo "Pod gpu-test1/pod0, container ctr0 should have a new GPU but claimed $gpu_test1_pod0_ctr0_gpu which is already claimed"
  exit 1
fi
echo "Pod gpu-test1/pod0, container ctr0 claimed $gpu_test1_pod0_ctr0_gpu"
observed_gpus+=("$gpu_test1_pod0_ctr0_gpu")

gpu_test1_pod1_ctr0_logs=$(kubectl logs -n gpu-test1 pod1 -c ctr0)
gpu_test1_pod1_ctr0_gpus=$(gpus-from-logs "$gpu_test1_pod1_ctr0_logs")
gpu_test1_pod1_ctr0_gpus_count=$(echo "$gpu_test1_pod1_ctr0_gpus" | wc -w)
if [[ $gpu_test1_pod1_ctr0_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test1/pod1, container ctr0 to have 1 GPU, but got $gpu_test1_pod1_ctr0_gpus_count: $gpu_test1_pod1_ctr0_gpus"
  exit 1
fi
gpu_test1_pod1_ctr0_gpu="$gpu_test1_pod1_ctr0_gpus"
if gpu-already-seen "$gpu_test1_pod1_ctr0_gpu"; then
  echo "Pod gpu-test1/pod1, container ctr0 should have a new GPU but claimed $gpu_test1_pod1_ctr0_gpu which is already claimed"
  exit 1
fi
echo "Pod gpu-test1/pod1, container ctr0 claimed $gpu_test1_pod1_ctr0_gpu"
observed_gpus+=("$gpu_test1_pod1_ctr0_gpu")


kubectl wait --for=condition=Ready -n gpu-test2 pod/pod0 --timeout=120s
gpu_test_2=$(kubectl get pods -n gpu-test2 | grep -c 'Running')
if [ $gpu_test_2 != 1 ]; then
    echo "gpu_test_2 $gpu_test_2 failed to match against 1 expected pod"
    exit 1
fi

gpu_test2_pod0_ctr0_logs=$(kubectl logs -n gpu-test2 pod0 -c ctr0)
gpu_test2_pod0_ctr0_gpus=$(gpus-from-logs "$gpu_test2_pod0_ctr0_logs")
gpu_test2_pod0_ctr0_gpus_count=$(echo "$gpu_test2_pod0_ctr0_gpus" | wc -w)
if [[ $gpu_test2_pod0_ctr0_gpus_count != 2 ]]; then
  echo "Expected Pod gpu-test2/pod0, container ctr0 to have 2 GPUs, but got $gpu_test2_pod0_ctr0_gpus_count: $gpu_test2_pod0_ctr0_gpus"
  exit 1
fi
echo "$gpu_test2_pod0_ctr0_gpus" | while read gpu_test2_pod0_ctr0_gpu; do
  if gpu-already-seen "$gpu_test2_pod0_ctr0_gpu"; then
    echo "Pod gpu-test2/pod0, container ctr0 should have a new GPU but claimed $gpu_test2_pod0_ctr0_gpu which is already claimed"
    exit 1
  fi
  echo "Pod gpu-test2/pod0, container ctr0 claimed $gpu_test2_pod0_ctr0_gpu"
  observed_gpus+=("$gpu_test2_pod0_ctr0_gpu")
done


kubectl wait --for=condition=Ready -n gpu-test3 pod/pod0 --timeout=120s
gpu_test_3=$(kubectl get pods -n gpu-test3 | grep -c 'Running')
if [ $gpu_test_3 != 1 ]; then
    echo "gpu_test_3 $gpu_test_3 failed to match against 1 expected pod"
    exit 1
fi

gpu_test3_pod0_ctr0_logs=$(kubectl logs -n gpu-test3 pod0 -c ctr0)
gpu_test3_pod0_ctr0_gpus=$(gpus-from-logs "$gpu_test3_pod0_ctr0_logs")
gpu_test3_pod0_ctr0_gpus_count=$(echo "$gpu_test3_pod0_ctr0_gpus" | wc -w)
if [[ $gpu_test3_pod0_ctr0_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test3/pod0, container ctr0 to have 1 GPU, but got $gpu_test3_pod0_ctr0_gpus_count: $gpu_test3_pod0_ctr0_gpus"
  exit 1
fi
gpu_test3_pod0_ctr0_gpu="$gpu_test3_pod0_ctr0_gpus"
if gpu-already-seen "$gpu_test3_pod0_ctr0_gpu"; then
  echo "Pod gpu-test3/pod0, container ctr0 should have a new GPU but claimed $gpu_test3_pod0_ctr0_gpu which is already claimed"
  exit 1
fi
echo "Pod gpu-test3/pod0, container ctr0 claimed $gpu_test3_pod0_ctr0_gpu"
observed_gpus+=("$gpu_test3_pod0_ctr0_gpu")
gpu_test3_pod0_ctr0_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test3_pod0_ctr0_logs" $(gpu-id "$gpu_test3_pod0_ctr0_gpu"))
if [[ "$gpu_test3_pod0_ctr0_sharing_strategy" != "TimeSlicing" ]]; then
  echo "Expected Pod gpu-test3/pod0, container ctr0 to have sharing strategy TimeSlicing, got $gpu_test3_pod0_ctr0_sharing_strategy"
  exit 1
fi
gpu_test3_pod0_ctr0_timeslice_interval=$(gpu-timeslice-interval-from-logs "$gpu_test3_pod0_ctr0_logs" $(gpu-id "$gpu_test3_pod0_ctr0_gpu"))
if [[ "$gpu_test3_pod0_ctr0_timeslice_interval" != "Default" ]]; then
  echo "Expected Pod gpu-test3/pod0, container ctr0 to have timeslice interval Default, got $gpu_test3_pod0_ctr0_timeslice_interval"
  exit 1
fi

gpu_test3_pod0_ctr1_logs=$(kubectl logs -n gpu-test3 pod0 -c ctr1)
gpu_test3_pod0_ctr1_gpus=$(gpus-from-logs "$gpu_test3_pod0_ctr1_logs")
gpu_test3_pod0_ctr1_gpus_count=$(echo "$gpu_test3_pod0_ctr1_gpus" | wc -w)
if [[ $gpu_test3_pod0_ctr1_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test3/pod0, container ctr1 to have 1 GPU, but got $gpu_test3_pod0_ctr1_gpus_count: $gpu_test3_pod0_ctr1_gpus"
  exit 1
fi
gpu_test3_pod0_ctr1_gpu="$gpu_test3_pod0_ctr1_gpus"
echo "Pod gpu-test3/pod0, container ctr1 claimed $gpu_test3_pod0_ctr1_gpu"
if [[ "$gpu_test3_pod0_ctr1_gpu" != "$gpu_test3_pod0_ctr0_gpu" ]]; then
  echo "Pod gpu-test3/pod0, container ctr1 should claim the same GPU as Pod gpu-test3/pod0, container ctr0, but did not"
  exit 1
fi
gpu_test3_pod0_ctr1_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test3_pod0_ctr1_logs" $(gpu-id "$gpu_test3_pod0_ctr1_gpu"))
if [[ "$gpu_test3_pod0_ctr1_sharing_strategy" != "TimeSlicing" ]]; then
  echo "Expected Pod gpu-test3/pod0, container ctr1 to have sharing strategy TimeSlicing, got $gpu_test3_pod0_ctr1_sharing_strategy"
  exit 1
fi
gpu_test3_pod0_ctr1_timeslice_interval=$(gpu-timeslice-interval-from-logs "$gpu_test3_pod0_ctr1_logs" $(gpu-id "$gpu_test3_pod0_ctr1_gpu"))
if [[ "$gpu_test3_pod0_ctr1_timeslice_interval" != "Default" ]]; then
  echo "Expected Pod gpu-test3/pod0, container ctr1 to have timeslice interval Default, got $gpu_test3_pod0_ctr1_timeslice_interval"
  exit 1
fi


kubectl wait --for=condition=Ready -n gpu-test4 pod/pod0 --timeout=120s
kubectl wait --for=condition=Ready -n gpu-test4 pod/pod1 --timeout=120s
gpu_test_4=$(kubectl get pods -n gpu-test4 | grep -c 'Running')
if [ $gpu_test_4 != 2 ]; then
    echo "gpu_test_4 $gpu_test_4 failed to match against 2 expected pods"
    exit 1
fi

gpu_test4_pod0_ctr0_logs=$(kubectl logs -n gpu-test4 pod0 -c ctr0)
gpu_test4_pod0_ctr0_gpus=$(gpus-from-logs "$gpu_test4_pod0_ctr0_logs")
gpu_test4_pod0_ctr0_gpus_count=$(echo "$gpu_test4_pod0_ctr0_gpus" | wc -w)
if [[ $gpu_test4_pod0_ctr0_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test4/pod0, container ctr0 to have 1 GPU, but got $gpu_test4_pod0_ctr0_gpus_count: $gpu_test4_pod0_ctr0_gpus"
  exit 1
fi
gpu_test4_pod0_ctr0_gpu="$gpu_test4_pod0_ctr0_gpus"
if gpu-already-seen "$gpu_test4_pod0_ctr0_gpu"; then
  echo "Pod gpu-test4/pod0, container ctr0 should have a new GPU but claimed $gpu_test4_pod0_ctr0_gpu which is already claimed"
  exit 1
fi
echo "Pod gpu-test4/pod0, container ctr0 claimed $gpu_test4_pod0_ctr0_gpu"
observed_gpus+=("$gpu_test4_pod0_ctr0_gpu")
gpu_test4_pod0_ctr0_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test4_pod0_ctr0_logs" $(gpu-id "$gpu_test4_pod0_ctr0_gpu"))
if [[ "$gpu_test4_pod0_ctr0_sharing_strategy" != "TimeSlicing" ]]; then
  echo "Expected Pod gpu-test4/pod0, container ctr0 to have sharing strategy TimeSlicing, got $gpu_test4_pod0_ctr0_sharing_strategy"
  exit 1
fi
gpu_test4_pod0_ctr0_timeslice_interval=$(gpu-timeslice-interval-from-logs "$gpu_test4_pod0_ctr0_logs" $(gpu-id "$gpu_test4_pod0_ctr0_gpu"))
if [[ "$gpu_test4_pod0_ctr0_timeslice_interval" != "Default" ]]; then
  echo "Expected Pod gpu-test4/pod0, container ctr0 to have timeslice interval Default, got $gpu_test4_pod0_ctr0_timeslice_interval"
  exit 1
fi

gpu_test4_pod1_ctr0_logs=$(kubectl logs -n gpu-test4 pod1 -c ctr0)
gpu_test4_pod1_ctr0_gpus=$(gpus-from-logs "$gpu_test4_pod1_ctr0_logs")
gpu_test4_pod1_ctr0_gpus_count=$(echo "$gpu_test4_pod1_ctr0_gpus" | wc -w)
if [[ $gpu_test4_pod1_ctr0_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test4/pod1, container ctr0 to have 1 GPU, but got $gpu_test4_pod1_ctr0_gpus_count: $gpu_test4_pod1_ctr0_gpus"
  exit 1
fi
gpu_test4_pod1_ctr0_gpu="$gpu_test4_pod1_ctr0_gpus"
echo "Pod gpu-test4/pod1, container ctr0 claimed $gpu_test4_pod1_ctr0_gpu"
if [[ "$gpu_test4_pod1_ctr0_gpu" != "$gpu_test4_pod1_ctr0_gpu" ]]; then
  echo "Pod gpu-test4/pod1, container ctr0 should claim the same GPU as Pod gpu-test4/pod1, container ctr0, but did not"
  exit 1
fi
gpu_test4_pod1_ctr0_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test4_pod1_ctr0_logs" $(gpu-id "$gpu_test4_pod1_ctr0_gpu"))
if [[ "$gpu_test4_pod1_ctr0_sharing_strategy" != "TimeSlicing" ]]; then
  echo "Expected Pod gpu-test4/pod1, container ctr0 to have sharing strategy TimeSlicing, got $gpu_test4_pod1_ctr0_sharing_strategy"
  exit 1
fi
gpu_test4_pod1_ctr0_timeslice_interval=$(gpu-timeslice-interval-from-logs "$gpu_test4_pod1_ctr0_logs" $(gpu-id "$gpu_test4_pod1_ctr0_gpu"))
if [[ "$gpu_test4_pod1_ctr0_timeslice_interval" != "Default" ]]; then
  echo "Expected Pod gpu-test4/pod1, container ctr0 to have timeslice interval Default, got $gpu_test4_pod1_ctr0_timeslice_interval"
  exit 1
fi


kubectl wait --for=condition=Ready -n gpu-test5 pod/pod0 --timeout=120s
gpu_test_5=$(kubectl get pods -n gpu-test5 | grep -c 'Running')
if [ $gpu_test_5 != 1 ]; then
    echo "gpu_test_5 $gpu_test_5 failed to match against 1 expected pod"
    exit 1
fi

gpu_test5_pod0_ts_ctr0_logs=$(kubectl logs -n gpu-test5 pod0 -c ts-ctr0)
gpu_test5_pod0_ts_ctr0_gpus=$(gpus-from-logs "$gpu_test5_pod0_ts_ctr0_logs")
gpu_test5_pod0_ts_ctr0_gpus_count=$(echo "$gpu_test5_pod0_ts_ctr0_gpus" | wc -w)
if [[ $gpu_test5_pod0_ts_ctr0_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test5/pod0, container ts-ctr0 to have 1 GPU, but got $gpu_test5_pod0_ts_ctr0_gpus_count: $gpu_test5_pod0_ts_ctr0_gpus"
  exit 1
fi
gpu_test5_pod0_ts_ctr0_gpu="$gpu_test5_pod0_ts_ctr0_gpus"
if gpu-already-seen "$gpu_test5_pod0_ts_ctr0_gpu"; then
  echo "Pod gpu-test5/pod0, container ts-ctr0 should have a new GPU but claimed $gpu_test5_pod0_ts_ctr0_gpu which is already claimed"
  exit 1
fi
echo "Pod gpu-test5/pod0, container ts-ctr0 claimed $gpu_test5_pod0_ts_ctr0_gpu"
observed_gpus+=("$gpu_test5_pod0_ts_ctr0_gpu")
gpu_test5_pod0_ts_ctr0_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test5_pod0_ts_ctr0_logs" $(gpu-id "$gpu_test5_pod0_ts_ctr0_gpu"))
if [[ "$gpu_test5_pod0_ts_ctr0_sharing_strategy" != "TimeSlicing" ]]; then
  echo "Expected Pod gpu-test5/pod0, container ts-ctr0 to have sharing strategy TimeSlicing, got $gpu_test5_pod0_ts_ctr0_sharing_strategy"
  exit 1
fi
gpu_test5_pod0_ts_ctr0_timeslice_interval=$(gpu-timeslice-interval-from-logs "$gpu_test5_pod0_ts_ctr0_logs" $(gpu-id "$gpu_test5_pod0_ts_ctr0_gpu"))
if [[ "$gpu_test5_pod0_ts_ctr0_timeslice_interval" != "Long" ]]; then
  echo "Expected Pod gpu-test5/pod0, container ts-ctr0 to have timeslice interval Long, got $gpu_test5_pod0_ts_ctr0_timeslice_interval"
  exit 1
fi

gpu_test5_pod0_ts_ctr1_logs=$(kubectl logs -n gpu-test5 pod0 -c ts-ctr1)
gpu_test5_pod0_ts_ctr1_gpus=$(gpus-from-logs "$gpu_test5_pod0_ts_ctr1_logs")
gpu_test5_pod0_ts_ctr1_gpus_count=$(echo "$gpu_test5_pod0_ts_ctr1_gpus" | wc -w)
if [[ $gpu_test5_pod0_ts_ctr1_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test5/pod0, container ts-ctr1 to have 1 GPU, but got $gpu_test5_pod0_ts_ctr1_gpus_count: $gpu_test5_pod0_ts_ctr1_gpus"
  exit 1
fi
gpu_test5_pod0_ts_ctr1_gpu="$gpu_test5_pod0_ts_ctr1_gpus"
echo "Pod gpu-test5/pod0, container ts-ctr1 claimed $gpu_test5_pod0_ts_ctr1_gpu"
if [[ "$gpu_test5_pod0_ts_ctr1_gpu" != "$gpu_test5_pod0_ts_ctr0_gpu" ]]; then
  echo "Pod gpu-test5/pod0, container ts-ctr1 should claim the same GPU as Pod gpu-test5/pod0, container ts-ctr0, but did not"
  exit 1
fi
gpu_test5_pod0_ts_ctr1_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test5_pod0_ts_ctr1_logs" $(gpu-id "$gpu_test5_pod0_ts_ctr1_gpu"))
if [[ "$gpu_test5_pod0_ts_ctr1_sharing_strategy" != "TimeSlicing" ]]; then
  echo "Expected Pod gpu-test5/pod0, container ts-ctr1 to have sharing strategy TimeSlicing, got $gpu_test5_pod0_ts_ctr1_sharing_strategy"
  exit 1
fi
gpu_test5_pod0_ts_ctr1_timeslice_interval=$(gpu-timeslice-interval-from-logs "$gpu_test5_pod0_ts_ctr1_logs" $(gpu-id "$gpu_test5_pod0_ts_ctr1_gpu"))
if [[ "$gpu_test5_pod0_ts_ctr1_timeslice_interval" != "Long" ]]; then
  echo "Expected Pod gpu-test5/pod0, container ts-ctr1 to have timeslice interval Long, got $gpu_test5_pod0_ts_ctr1_timeslice_interval"
  exit 1
fi

gpu_test5_pod0_sp_ctr0_logs=$(kubectl logs -n gpu-test5 pod0 -c sp-ctr0)
gpu_test5_pod0_sp_ctr0_gpus=$(gpus-from-logs "$gpu_test5_pod0_sp_ctr0_logs")
gpu_test5_pod0_sp_ctr0_gpus_count=$(echo "$gpu_test5_pod0_sp_ctr0_gpus" | wc -w)
if [[ $gpu_test5_pod0_sp_ctr0_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test5/pod0, container sp-ctr0 to have 1 GPU, but got $gpu_test5_pod0_sp_ctr0_gpus_count: $gpu_test5_pod0_sp_ctr0_gpus"
  exit 1
fi
gpu_test5_pod0_sp_ctr0_gpu="$gpu_test5_pod0_sp_ctr0_gpus"
if gpu-already-seen "$gpu_test5_pod0_sp_ctr0_gpu"; then
  echo "Pod gpu-test5/pod0, container sp-ctr0 should have a new GPU but claimed $gpu_test5_pod0_sp_ctr0_gpu which is already claimed"
  exit 1
fi
echo "Pod gpu-test5/pod0, container sp-ctr0 claimed $gpu_test5_pod0_sp_ctr0_gpu"
observed_gpus+=("$gpu_test5_pod0_sp_ctr0_gpu")
gpu_test5_pod0_sp_ctr0_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test5_pod0_sp_ctr0_logs" $(gpu-id "$gpu_test5_pod0_sp_ctr0_gpu"))
if [[ "$gpu_test5_pod0_sp_ctr0_sharing_strategy" != "SpacePartitioning" ]]; then
  echo "Expected Pod gpu-test5/pod0, container sp-ctr0 to have sharing strategy SpacePartitioning, got $gpu_test5_pod0_sp_ctr0_sharing_strategy"
  exit 1
fi
gpu_test5_pod0_sp_ctr0_partition_count=$(gpu-partition-count-from-logs "$gpu_test5_pod0_sp_ctr0_logs" $(gpu-id "$gpu_test5_pod0_sp_ctr0_gpu"))
if [[ "$gpu_test5_pod0_sp_ctr0_partition_count" != "10" ]]; then
  echo "Expected Pod gpu-test5/pod0, container sp-ctr0 to have partition count 10, got $gpu_test5_pod0_sp_ctr0_partition_count"
  exit 1
fi

gpu_test5_pod0_sp_ctr1_logs=$(kubectl logs -n gpu-test5 pod0 -c sp-ctr1)
gpu_test5_pod0_sp_ctr1_gpus=$(gpus-from-logs "$gpu_test5_pod0_sp_ctr1_logs")
gpu_test5_pod0_sp_ctr1_gpus_count=$(echo "$gpu_test5_pod0_sp_ctr1_gpus" | wc -w)
if [[ $gpu_test5_pod0_sp_ctr1_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test5/pod0, container sp-ctr1 to have 1 GPU, but got $gpu_test5_pod0_sp_ctr1_gpus_count: $gpu_test5_pod0_sp_ctr1_gpus"
  exit 1
fi
gpu_test5_pod0_sp_ctr1_gpu="$gpu_test5_pod0_sp_ctr1_gpus"
echo "Pod gpu-test5/pod0, container sp-ctr1 claimed $gpu_test5_pod0_sp_ctr1_gpu"
if [[ "$gpu_test5_pod0_sp_ctr1_gpu" != "$gpu_test5_pod0_sp_ctr0_gpu" ]]; then
  echo "Pod gpu-test5/pod0, container sp-ctr1 should claim the same GPU as Pod gpu-test5/pod0, container sp-ctr0, but did not"
  exit 1
fi
gpu_test5_pod0_sp_ctr1_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test5_pod0_sp_ctr1_logs" $(gpu-id "$gpu_test5_pod0_sp_ctr1_gpu"))
if [[ "$gpu_test5_pod0_sp_ctr1_sharing_strategy" != "SpacePartitioning" ]]; then
  echo "Expected Pod gpu-test5/pod0, container sp-ctr1 to have sharing strategy SpacePartitioning, got $gpu_test5_pod0_sp_ctr1_sharing_strategy"
  exit 1
fi
gpu_test5_pod0_sp_ctr1_partition_count=$(gpu-partition-count-from-logs "$gpu_test5_pod0_sp_ctr1_logs" $(gpu-id "$gpu_test5_pod0_sp_ctr1_gpu"))
if [[ "$gpu_test5_pod0_sp_ctr1_partition_count" != "10" ]]; then
  echo "Expected Pod gpu-test5/pod0, container sp-ctr1 to have partition count 10, got $gpu_test5_pod0_sp_ctr1_partition_count"
  exit 1
fi

kubectl wait --for=condition=Ready -n gpu-test6 pod/pod0 --timeout=120s
gpu_test_6=$(kubectl get pods -n gpu-test6 | grep -c 'Running')
if [ $gpu_test_6 != 1 ]; then
    echo "gpu_test_6 $gpu_test_6 failed to match against 1 expected pod"
    exit 1
fi

gpu_test6_pod0_init0_logs=$(kubectl logs -n gpu-test6 pod0 -c init0)
gpu_test6_pod0_init0_gpus=$(gpus-from-logs "$gpu_test6_pod0_init0_logs")
gpu_test6_pod0_init0_gpus_count=$(echo "$gpu_test6_pod0_init0_gpus" | wc -w)
if [[ $gpu_test6_pod0_init0_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test6/pod0, container init0 to have 1 GPU, but got $gpu_test6_pod0_init0_gpus_count: $gpu_test6_pod0_init0_gpus"
  exit 1
fi
gpu_test6_pod0_init0_gpu="$gpu_test6_pod0_init0_gpus"
if gpu-already-seen "$gpu_test6_pod0_init0_gpu"; then
  echo "Pod gpu-test6/pod0, container init0 should have a new GPU but claimed $gpu_test6_pod0_init0_gpu which is already claimed"
  exit 1
fi
echo "Pod gpu-test6/pod0, container init0 claimed $gpu_test6_pod0_init0_gpu"
observed_gpus+=("$gpu_test6_pod0_init0_gpu")
gpu_test6_pod0_init0_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test6_pod0_init0_logs" $(gpu-id "$gpu_test6_pod0_init0_gpu"))
if [[ "$gpu_test6_pod0_init0_sharing_strategy" != "TimeSlicing" ]]; then
  echo "Expected Pod gpu-test6/pod0, container init0 to have sharing strategy TimeSlicing, got $gpu_test6_pod0_init0_sharing_strategy"
  exit 1
fi
gpu_test6_pod0_init0_timeslice_interval=$(gpu-timeslice-interval-from-logs "$gpu_test6_pod0_init0_logs" $(gpu-id "$gpu_test6_pod0_init0_gpu"))
if [[ "$gpu_test6_pod0_init0_timeslice_interval" != "Default" ]]; then
  echo "Expected Pod gpu-test6/pod0, container init0 to have timeslice interval Default, got $gpu_test6_pod0_init0_timeslice_interval"
  exit 1
fi

gpu_test6_pod0_ctr0_logs=$(kubectl logs -n gpu-test6 pod0 -c ctr0)
gpu_test6_pod0_ctr0_gpus=$(gpus-from-logs "$gpu_test6_pod0_ctr0_logs")
gpu_test6_pod0_ctr0_gpus_count=$(echo "$gpu_test6_pod0_ctr0_gpus" | wc -w)
if [[ $gpu_test6_pod0_ctr0_gpus_count != 1 ]]; then
  echo "Expected Pod gpu-test6/pod0, container ctr0 to have 1 GPU, but got $gpu_test6_pod0_ctr0_gpus_count: $gpu_test6_pod0_ctr0_gpus"
  exit 1
fi
gpu_test6_pod0_ctr0_gpu="$gpu_test6_pod0_ctr0_gpus"
echo "Pod gpu-test6/pod0, container ctr0 claimed $gpu_test6_pod0_ctr0_gpu"
if [[ "$gpu_test6_pod0_ctr0_gpu" != "$gpu_test6_pod0_init0_gpu" ]]; then
  echo "Pod gpu-test6/pod0, container ctr0 should claim the same GPU as Pod gpu-test6/pod0, container init0, but did not"
  exit 1
fi
gpu_test6_pod0_ctr0_sharing_strategy=$(gpu-sharing-strategy-from-logs "$gpu_test6_pod0_ctr0_logs" $(gpu-id "$gpu_test6_pod0_ctr0_gpu"))
if [[ "$gpu_test6_pod0_ctr0_sharing_strategy" != "TimeSlicing" ]]; then
  echo "Expected Pod gpu-test6/pod0, container ctr0 to have sharing strategy TimeSlicing, got $gpu_test6_pod0_ctr0_sharing_strategy"
  exit 1
fi
gpu_test6_pod0_ctr0_timeslice_interval=$(gpu-timeslice-interval-from-logs "$gpu_test6_pod0_ctr0_logs" $(gpu-id "$gpu_test6_pod0_ctr0_gpu"))
if [[ "$gpu_test6_pod0_ctr0_timeslice_interval" != "Default" ]]; then
  echo "Expected Pod gpu-test6/pod0, container ctr0 to have timeslice interval Default, got $gpu_test6_pod0_ctr0_timeslice_interval"
  exit 1
fi

# test that deletion is fast (less than the default grace period of 30s)
# see https://github.com/kubernetes/kubernetes/issues/127188 for details
kubectl delete -f demo/gpu-test1.yaml --timeout=25s
kubectl delete -f demo/gpu-test2.yaml --timeout=25s
kubectl delete -f demo/gpu-test3.yaml --timeout=25s
kubectl delete -f demo/gpu-test4.yaml --timeout=25s
kubectl delete -f demo/gpu-test5.yaml --timeout=25s
kubectl delete -f demo/gpu-test6.yaml --timeout=25s

# Webhook should reject invalid resources
if ! kubectl create --dry-run=server -f- <<'EOF' 2>&1 | grep -qF 'unknown time-slice interval'
apiVersion: resource.k8s.io/v1beta1
kind: ResourceClaim
metadata:
  name: webhook-test
spec:
  devices:
    requests:
    - name: ts-gpu
      deviceClassName: gpu.example.com
    - name: sp-gpu
      deviceClassName: gpu.example.com
    config:
    - requests: ["ts-gpu"]
      opaque:
        driver: gpu.example.com
        parameters:
          apiVersion: gpu.resource.example.com/v1alpha1
          kind: GpuConfig
          sharing:
            strategy: TimeSlicing
            timeSlicingConfig:
              interval: InvalidInterval
EOF
then
  echo "Webhook did not reject ResourceClaim invalid GpuConfig with the expected message"
  exit 1
fi

if ! kubectl create --dry-run=server -f- <<'EOF' 2>&1 | grep -qF 'unknown time-slice interval'
apiVersion: resource.k8s.io/v1beta1
kind: ResourceClaimTemplate
metadata:
  name: webhook-test
spec:
  spec:
    devices:
      requests:
      - name: ts-gpu
        deviceClassName: gpu.example.com
      - name: sp-gpu
        deviceClassName: gpu.example.com
      config:
      - requests: ["ts-gpu"]
        opaque:
          driver: gpu.example.com
          parameters:
            apiVersion: gpu.resource.example.com/v1alpha1
            kind: GpuConfig
            sharing:
              strategy: TimeSlicing
              timeSlicingConfig:
                interval: InvalidInterval
EOF
then
  echo "Webhook did not reject ResourceClaimTemplate invalid GpuConfig with the expected message"
  exit 1
fi

echo "test ran successfully"
