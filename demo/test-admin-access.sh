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

# This script demonstrates the DRA Admin Access feature by deploying
# the demo and verifying the DRA_ADMIN_ACCESS environment variable is set

set -e

echo "=== DRA Admin Access Feature Test ==="
echo

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl is not available. Please install kubectl and ensure cluster access."
    exit 1
fi

# Check if the cluster is accessible
if ! kubectl cluster-info &> /dev/null; then
    echo "❌ Unable to access Kubernetes cluster. Please check your kubeconfig."
    exit 1
fi

echo "✅ Kubernetes cluster is accessible"

# Apply the demo
echo "📦 Applying gpu-test7.yaml demo..."
kubectl apply -f demo/gpu-test7.yaml

echo "⏳ Waiting for pod to be ready..."
kubectl wait --for=condition=Ready pod/pod0 -n gpu-test7 --timeout=120s || true

echo
echo "=== Pod Status ==="
kubectl get pods -n gpu-test7

echo
echo "=== ResourceClaims Status ==="
kubectl get resourceclaims -n gpu-test7

echo
echo "=== Pod0 Logs (showing admin access demo) ==="
kubectl logs pod0 -n gpu-test7 || echo "⚠️  Pod0 logs not ready yet"

echo
echo "=== Checking DRA_ADMIN_ACCESS Environment Variable ==="
DRA_ADMIN_ACCESS_POD0=$(kubectl exec pod0 -n gpu-test7 -- printenv DRA_ADMIN_ACCESS 2>/dev/null || echo "not found")

if [[ "$DRA_ADMIN_ACCESS_POD0" == "true" ]]; then
  echo "✅ Pod0: DRA_ADMIN_ACCESS=$DRA_ADMIN_ACCESS_POD0"
else
  echo "❌ Pod0: DRA_ADMIN_ACCESS=$DRA_ADMIN_ACCESS_POD0 (expected: true)"
fi

echo
echo "=== Test Complete ==="
echo "To clean up, run: kubectl delete namespace gpu-test7"
