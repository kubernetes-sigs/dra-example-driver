#!/bin/bash

# DRA Admin Access Feature Test Script
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

echo "⏳ Waiting for pods to be ready..."
kubectl wait --for=condition=Ready pod/pod0 -n gpu-test7 --timeout=120s || true
kubectl wait --for=condition=Ready pod/pod1 -n gpu-test7 --timeout=120s || true

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
echo "=== Pod1 Logs (showing admin access demo) ==="
kubectl logs pod1 -n gpu-test7 || echo "⚠️  Pod1 logs not ready yet"

echo
echo "=== Checking DRA_ADMIN_ACCESS Environment Variable ==="
DRA_ADMIN_ACCESS_POD0=$(kubectl exec pod0 -n gpu-test7 -- printenv DRA_ADMIN_ACCESS 2>/dev/null || echo "not found")
DRA_ADMIN_ACCESS_POD1=$(kubectl exec pod1 -n gpu-test7 -- printenv DRA_ADMIN_ACCESS 2>/dev/null || echo "not found")

if [[ "$DRA_ADMIN_ACCESS_POD0" == "true" ]]; then
  echo "✅ Pod0: DRA_ADMIN_ACCESS=$DRA_ADMIN_ACCESS_POD0"
else
  echo "❌ Pod0: DRA_ADMIN_ACCESS=$DRA_ADMIN_ACCESS_POD0 (expected: true)"
fi

if [[ "$DRA_ADMIN_ACCESS_POD1" == "true" ]]; then
  echo "✅ Pod1: DRA_ADMIN_ACCESS=$DRA_ADMIN_ACCESS_POD1"
else
  echo "❌ Pod1: DRA_ADMIN_ACCESS=$DRA_ADMIN_ACCESS_POD1 (expected: true)"
fi

echo
echo "=== Checking Host Hardware Info Environment Variables (Pod0) ==="
kubectl exec pod0 -n gpu-test7 -- printenv | grep -E "^HOST_" | sort || echo "⚠️  No HOST_* variables found"

echo
echo "=== Verifying Network Interfaces Environment Variable ==="
HOST_NETWORK_INTERFACES=$(kubectl exec pod0 -n gpu-test7 -- printenv HOST_NETWORK_INTERFACES 2>/dev/null || echo "not found")
if [[ "$HOST_NETWORK_INTERFACES" != "not found" ]] && [[ "$HOST_NETWORK_INTERFACES" != "none" ]]; then
  echo "✅ Pod0: HOST_NETWORK_INTERFACES=$HOST_NETWORK_INTERFACES"
else
  echo "⚠️  Pod0: HOST_NETWORK_INTERFACES=$HOST_NETWORK_INTERFACES"
fi

echo
echo "=== Test Complete ==="
echo "To clean up, run: kubectl delete namespace gpu-test7"
