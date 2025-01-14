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

kubectl wait --for=condition=Ready -n gpu-test1 pod/pod0 --timeout=120s
kubectl wait --for=condition=Ready -n gpu-test1 pod/pod1 --timeout=120s
gpu_test_1=$(kubectl get pods -n gpu-test1 | grep -c 'Running')
if [ $gpu_test_1 != 2 ]; then
    echo "gpu_test_1 $gpu_test_1 failed to match against 2 expected pods"
    exit 1
fi


kubectl wait --for=condition=Ready -n gpu-test2 pod/pod0 --timeout=120s
gpu_test_2=$(kubectl get pods -n gpu-test2 | grep -c 'Running')
if [ $gpu_test_2 != 1 ]; then
    echo "gpu_test_2 $gpu_test_2 failed to match against 1 expected pod"
    exit 1
fi

kubectl wait --for=condition=Ready -n gpu-test3 pod/pod0 --timeout=120s
gpu_test_3=$(kubectl get pods -n gpu-test3 | grep -c 'Running')
if [ $gpu_test_3 != 1 ]; then
    echo "gpu_test_3 $gpu_test_3 failed to match against 1 expected pod"
    exit 1
fi

kubectl wait --for=condition=Ready -n gpu-test4 pod/pod0 --timeout=120s
kubectl wait --for=condition=Ready -n gpu-test4 pod/pod1 --timeout=120s
gpu_test_4=$(kubectl get pods -n gpu-test4 | grep -c 'Running')
if [ $gpu_test_4 != 2 ]; then
    echo "gpu_test_4 $gpu_test_4 failed to match against 1 expected pods"
    exit 1
fi

kubectl wait --for=condition=Ready -n gpu-test5 pod/pod0 --timeout=120s
gpu_test_5=$(kubectl get pods -n gpu-test5 | grep -c 'Running')
if [ $gpu_test_5 != 1 ]; then
    echo "gpu_test_5 $gpu_test_5 failed to match against 1 expected pod"
    exit 1
fi

# test that deletion is fast (less than the default grace period of 30s)
# see https://github.com/kubernetes/kubernetes/issues/127188 for details
kubectl delete -f demo/gpu-test1.yaml --timeout=25s
kubectl delete -f demo/gpu-test2.yaml --timeout=25s
kubectl delete -f demo/gpu-test3.yaml --timeout=25s
kubectl delete -f demo/gpu-test4.yaml --timeout=25s
kubectl delete -f demo/gpu-test5.yaml --timeout=25s

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
