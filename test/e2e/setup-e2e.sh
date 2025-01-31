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

bash demo/build-driver.sh
bash demo/create-cluster.sh

helm upgrade -i \
  --repo https://charts.jetstack.io \
  --version v1.16.3 \
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
  dra-example-driver \
  deployments/helm/dra-example-driver
