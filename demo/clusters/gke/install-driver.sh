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

set -euo pipefail

: "${DRIVER_RELEASE_NAME:=dra-example-driver}"
: "${DRIVER_NAMESPACE:=dra-example-driver}"
: "${DRIVER_VERSION:=0.2.0}"
: "${DRIVER_CHART_REF:=oci://registry.k8s.io/dra-example-driver/charts/dra-example-driver}"

helm upgrade -i \
  --create-namespace \
  --namespace "${DRIVER_NAMESPACE}" \
  --version "${DRIVER_VERSION}" \
  --set resourceQuota.enabled=true \
  --set image.tag="${DRIVER_VERSION}" \
  "${DRIVER_RELEASE_NAME}" \
  "${DRIVER_CHART_REF}"

echo "Driver install/upgrade complete: ${DRIVER_RELEASE_NAME} (${DRIVER_NAMESPACE}), version ${DRIVER_VERSION}"
