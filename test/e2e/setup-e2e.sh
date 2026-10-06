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

# Brings up the e2e cluster for `make test-e2e`. Everything is driven through the
# Makefile's kind-*/kube-ovn targets, so all cluster and kube-ovn Helm values are
# sourced from a single place (the Makefile) — this script only orders them.
#
# Set E2E_CONTAINERLAB=1 to also wire the VLAN underlay (needs sudo + containerlab),
# which enables the Label("containerlab") specs.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

make kind-create
if [ "${E2E_CONTAINERLAB:-0}" = "1" ]; then
  make clab-deploy
fi
make kind-deploy-kube-ovn
make kind-deploy-nic-prereqs
make kind-build-driver
make kind-deploy-driver
make wait-for-nic-devices

# The e2e suite applies/removes its own ResourceClaim+pod fixture, so the base
# demo claim/pod (kind-deploy-nic-example) is intentionally NOT deployed here.

echo "e2e cluster ready. Run: make test-e2e"
