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

kubectl create -f demo/gpu-test1.yaml
kubectl create -f demo/gpu-test2.yaml
kubectl create -f demo/gpu-test3.yaml
kubectl create -f demo/gpu-test4.yaml
kubectl create -f demo/gpu-test5.yaml
sleep 30

# stop at first failure to save time, error on undefined env variables
set -eu

gpu_test_1=$(kubectl get pods -n gpu-test1 | grep -c 'Running')
if [ $gpu_test_1 != 2 ]; then
    echo "gpu_test_1 $gpu_test_1 failed to match against 2 expected pods"
    exit 1
fi

gpu_test_2=$(kubectl get pods -n gpu-test2 | grep -c 'Running')
if [ $gpu_test_2 != 1 ]; then
    echo "gpu_test_2 $gpu_test_2 failed to match against 1 expected pod"
    exit 1
fi

gpu_test_3=$(kubectl get pods -n gpu-test3 | grep -c 'Running')
if [ $gpu_test_3 != 1 ]; then
    echo "gpu_test_3 $gpu_test_3 failed to match against 1 expected pod"
    exit 1
fi

gpu_test_4=$(kubectl get pods -n gpu-test4 | grep -c 'Running')
if [ $gpu_test_4 != 2 ]; then
    echo "gpu_test_4 $gpu_test_4 failed to match against 1 expected pods"
    exit 1
fi

gpu_test_5=$(kubectl get pods -n gpu-test5 | grep -c 'Running')
if [ $gpu_test_5 != 1 ]; then
    echo "gpu_test_5 $gpu_test_5 failed to match against 1 expected pod"
    exit 1
fi

echo "test ran successfully"
