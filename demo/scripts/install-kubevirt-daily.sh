#!/usr/bin/env bash

# Copyright The Kubernetes Authors.
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

# This script installs Kubevirt daily on the cluster


# NOTE: Using minikube or kind may not work out of the box in some hosts
#       because the sysctl fs.ionotify limits are too low.
#       Please follow this GitHub issue to fix it
#       https://github.com/kubernetes/minikube/issues/18831

set -ex
set -o pipefail

# Install Kubevirt
VERSION=$(curl -s https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/latest)
kubectl apply -f https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/${VERSION}/kubevirt-operator.yaml
kubectl apply -f https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/${VERSION}/kubevirt-cr.yaml

# Wait for Kubevirt to be ready
kubectl -n kubevirt wait kv kubevirt --for condition=Available --timeout=600s

# Enable DRA feature gates
kubectl patch kubevirt kubevirt -n kubevirt --type=merge -p '{"spec":{"configuration":{"developerConfiguration":{"featureGates":["GPUsWithDRA","HostDevicesWithDRA", "HostDevices"]}}}}'