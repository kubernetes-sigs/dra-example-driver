#!/bin/bash

# DRA Admin Access Feature Test Script
# This script demonstrates the DRA Admin Access feature by deploying
# the demo and checking the results

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
echo "=== Pod0 Logs (showing host hardware information) ==="
kubectl logs pod0 -n gpu-test7 || echo "⚠️  Pod0 logs not ready yet"

echo
echo "=== Pod1 Logs (showing host hardware information) ==="
kubectl logs pod1 -n gpu-test7 || echo "⚠️  Pod1 logs not ready yet"

echo
echo "=== Environment Variables in Pod0 ==="
kubectl exec pod0 -n gpu-test7 -- env | grep -E "(HOST_|DRA_|GPU_)" | sort || echo "⚠️  Pod0 not ready for exec"

echo
echo "=== Test Complete ==="
echo "To clean up, run: kubectl delete namespace gpu-test7"
