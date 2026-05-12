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

# This script demonstrates the DRA Device Binding Conditions feature by:
#   1. Deploying the demo manifest (demo/binding-conditions.yaml)
#   2. Verifying the pod stays Pending until binding conditions are met
#   3. Patching the ResourceClaim status to satisfy the binding condition
#   4. Verifying the pod transitions to Running

set -e

NAMESPACE="binding-conditions"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=== DRA Device Binding Conditions Feature Demo ==="
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
echo

# Verify ResourceSlice has binding condition fields
echo "=== ResourceSlice binding condition fields ==="
kubectl get resourceslice -o jsonpath='{range .items[*]}{range .spec.devices[*]}{.name}: bindsToNode={.bindsToNode}, bindingConditions={.bindingConditions}, bindingFailureConditions={.bindingFailureConditions}{"\n"}{end}{end}'
echo
echo

# Apply the demo manifest
echo "📦 Applying binding-conditions.yaml demo..."
kubectl apply -f "${SCRIPT_DIR}/binding-conditions.yaml"
echo

# Verify the pod is Pending (binding conditions not yet satisfied)
echo "⏳ Waiting briefly for the pod to be scheduled and stay Pending..."
sleep 5

echo
echo "=== Pod Status (expected: Pending) ==="
kubectl get pod pod0 -n "${NAMESPACE}"

POD_PHASE=$(kubectl get pod pod0 -n "${NAMESPACE}" -o jsonpath='{.status.phase}')
if [[ "${POD_PHASE}" == "Pending" ]]; then
    echo "✅ pod0 is Pending as expected (binding conditions not yet satisfied)"
else
    echo "⚠️  pod0 phase is '${POD_PHASE}' (expected Pending)"
fi
echo

# Find the ResourceClaim created for pod0
echo "=== ResourceClaim Status ==="
RC_NAME=$(kubectl get resourceclaim -n "${NAMESPACE}" -o jsonpath='{.items[0].metadata.name}')
if [[ -z "${RC_NAME}" ]]; then
    echo "❌ No ResourceClaim found in namespace ${NAMESPACE}"
    exit 1
fi
echo "ResourceClaim: ${RC_NAME}"
kubectl get resourceclaim -n "${NAMESPACE}" "${RC_NAME}"
echo
echo "--- ResourceClaim status (allocation and reservedFor) ---"
kubectl get resourceclaim -n "${NAMESPACE}" "${RC_NAME}" -o json | jq '{
  status: {
    allocation: {
      devices: {
        results: [.status.allocation.devices.results[] | {
          bindingConditions,
          bindingFailureConditions,
          device, driver, pool, request
        }]
      }
    },
    reservedFor: [.status.reservedFor[] | {name, resource}]
  }
}'
echo

# Patch the ResourceClaim status to satisfy the binding condition
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
DEVICE=$(kubectl get resourceclaim -n "${NAMESPACE}" "${RC_NAME}" \
    -o jsonpath='{.status.allocation.devices.results[0].device}')
DRIVER=$(kubectl get resourceclaim -n "${NAMESPACE}" "${RC_NAME}" \
    -o jsonpath='{.status.allocation.devices.results[0].driver}')
POOL=$(kubectl get resourceclaim -n "${NAMESPACE}" "${RC_NAME}" \
    -o jsonpath='{.status.allocation.devices.results[0].pool}')

echo "🔧 Patching ResourceClaim status to satisfy BindingConditions..."
echo "   Device: ${DEVICE}, Driver: ${DRIVER}, Pool: ${POOL}, Timestamp: ${TIMESTAMP}"

kubectl patch resourceclaim -n "${NAMESPACE}" "${RC_NAME}" \
    --subresource=status \
    --type=merge \
    -p "{
  \"status\": {
    \"devices\": [
      {
        \"conditions\": [
          {
            \"lastTransitionTime\": \"${TIMESTAMP}\",
            \"message\": \"Device ${DEVICE} condition BindingConditions updated\",
            \"reason\": \"BindingConditionsUpdated\",
            \"status\": \"True\",
            \"type\": \"BindingConditions\"
          }
        ],
        \"device\": \"${DEVICE}\",
        \"driver\": \"${DRIVER}\",
        \"pool\": \"${POOL}\"
      }
    ]
  }
}"
echo

# Wait for the pod to become Running
echo "⏳ Waiting for pod0 to become Running..."
kubectl wait --for=condition=Ready pod/pod0 -n "${NAMESPACE}" --timeout=60s

echo
echo "=== Pod Status (expected: Running) ==="
kubectl get pod pod0 -n "${NAMESPACE}"

POD_PHASE=$(kubectl get pod pod0 -n "${NAMESPACE}" -o jsonpath='{.status.phase}')
if [[ "${POD_PHASE}" == "Running" ]]; then
    echo "✅ pod0 is Running — binding conditions were satisfied successfully"
else
    echo "❌ pod0 phase is '${POD_PHASE}' (expected Running)"
    exit 1
fi
echo

echo
echo "=== Demo Complete ==="
echo "To clean up, run: kubectl delete namespace ${NAMESPACE}"
