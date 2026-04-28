#!/usr/bin/env bash

# Copyright 2026 The Kubernetes Authors.
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

set -euo pipefail

: "${GKE_CLUSTER_NAME:=dra-example-driver-cluster}"
: "${GKE_LOCATION:=us-central1-c}"
: "${GKE_RELEASE_CHANNEL:=rapid}"
: "${GKE_NUM_NODES:=1}"
: "${GKE_ENABLE_K8S_UNSTABLE_APIS:=resource.k8s.io/v1beta1/deviceclasses,resource.k8s.io/v1beta1/resourceclaims,resource.k8s.io/v1beta1/resourceclaimtemplates,resource.k8s.io/v1beta1/resourceslices}"

gcloud container clusters create "${GKE_CLUSTER_NAME}" \
  --location="${GKE_LOCATION}" \
  --release-channel="${GKE_RELEASE_CHANNEL}" \
  --num-nodes="${GKE_NUM_NODES}" \
  --enable-kubernetes-unstable-apis="${GKE_ENABLE_K8S_UNSTABLE_APIS}"

gcloud container clusters get-credentials "${GKE_CLUSTER_NAME}" --location "${GKE_LOCATION}"

echo "GKE cluster creation complete: ${GKE_CLUSTER_NAME}"
