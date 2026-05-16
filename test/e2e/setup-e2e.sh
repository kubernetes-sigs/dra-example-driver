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
#
# stop at first failure to save time
set -e

# Use local Helm chart by default, or from OCI registry if HELM_CHART_PATH is set
# Example: HELM_CHART_PATH="oci://registry.k8s.io/dra-example-driver/charts/dra-example-driver" make setup-e2e
HELM_CHART_PATH="${HELM_CHART_PATH:-deployments/helm/dra-example-driver}"

# Enable BindingConditions controller plugin and kubelet plugin flag when requested.
# Usage: BINDING_CONDITIONS=true make setup-e2e
BINDING_CONDITIONS_OPTS=""
if [[ "${BINDING_CONDITIONS:-false}" == "true" ]]; then
	BINDING_CONDITIONS_OPTS="--set kubeletPlugin.bindingConditions=true --set controller.plugins={BindingConditions}"
fi

# Skip building local driver image if using OCI registry chart
if [[ "${HELM_CHART_PATH}" != oci://* ]]; then
	bash demo/build-driver.sh
fi
bash demo/clusters/kind/create-cluster.sh

helm upgrade -i \
  --repo https://charts.jetstack.io \
  --version v1.20.2 \
  --create-namespace \
  --namespace cert-manager \
  --wait \
  --set crds.enabled=true \
  cert-manager \
  cert-manager

helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  --set webhook.enabled=true \
  --set kubeletPlugin.numDevices=14 \
  --set deviceClass.extendedResourceName=example.com/gpu \
  ${BINDING_CONDITIONS_OPTS} \
  dra-example-driver \
  ${HELM_CHART_PATH}
