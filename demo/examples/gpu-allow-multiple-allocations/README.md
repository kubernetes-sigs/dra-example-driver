# GPU Allow Multiple Allocations Example

## Overview

This example demonstrates the **DRAConsumableCapacity** feature, which allows multiple pods to share a single GPU by consuming slices of its capacity. Each pod requests a portion of the GPU's `memory` and `compute` counters from the same physical device simultaneously.

**Setup**: Two pods, each with one container, each consuming 16Gi memory and 20 compute units from the same GPU.

## What This Example Shows

- How to use `capacity.requests` in a ResourceClaim to consume a slice of a device's capacity
- Multiple pods sharing a single GPU via `AllowMultipleAllocations`
- The `DRAConsumableCapacity` feature gate in practice

## GPU Allocation

```mermaid
graph TD
    subgraph Node["Node (Physical Hardware)"]
        subgraph GPU1["GPU 1"]
            CAP["Capacity Pool<br/>memory: 80Gi total<br/>compute: 100 total"]
            S1["Slice: pod0<br/>memory: 16Gi / compute: 20"]
            S2["Slice: pod1<br/>memory: 16Gi / compute: 20"]
            CAP -.consumes.-> S1
            CAP -.consumes.-> S2
        end
    end

    subgraph Pod1 [Pod 0]
        C1(ctr0)
    end
    subgraph Pod2 [Pod 1]
        C2(ctr0)
    end

    S1 --- C1
    S2 --- C2

    style Node fill:#e3f2fd,stroke:#333,stroke-width:2px,color:#000
    style GPU1 fill:#e8f4f8,stroke:#326ce5,stroke-width:2px,color:#000
    style CAP fill:#fff3cd,color:#000,stroke:#856404,stroke-width:2px
    style S1 fill:#9b59b6,color:#fff,stroke:#8e44ad,stroke-width:2px
    style S2 fill:#3498db,color:#fff,stroke:#2980b9,stroke-width:2px
    style Pod1 stroke:#326ce5,stroke-width:2px
    style Pod2 stroke:#326ce5,stroke-width:2px
    style C1 fill:#d4edda,color:#000,stroke:#28a745,stroke-width:2px
    style C2 fill:#d4edda,color:#000,stroke:#28a745,stroke-width:2px
```

## Requirements

### Driver Requirements

- **Profile**: gpu
- **GPUs**: 1
- Helm value: `gpuAllowMultipleAllocations=true`

### Cluster Requirements

- Kubernetes 1.35+
- Feature gate: `DRAConsumableCapacity` (Alpha in 1.35, enabled by default in 1.36+)

## Prerequisites

Install the driver with multiple allocations enabled:

```bash
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  --set gpuAllowMultipleAllocations=true \
  dra-example-driver \
  deployments/helm/dra-example-driver
```

## How to Run

1. Apply the example:

   ```bash
   cd demo/examples/gpu-allow-multiple-allocations && kubectl apply -f gpu-allow-multiple-allocations.yaml
   ```

2. Verify both pods are running:

   ```bash
   kubectl get pods -n gpu-allow-multiple-allocations
   ```

3. Check GPU allocation for both pods:

   ```bash
   kubectl logs -n gpu-allow-multiple-allocations pod0 -c ctr0 | grep GPU_DEVICE
   kubectl logs -n gpu-allow-multiple-allocations pod1 -c ctr0 | grep GPU_DEVICE
   ```

## Expected Output

Both pods should show the **same** GPU ID, confirming they are sharing the same physical GPU, each consuming their requested capacity slice.

Example output:

```
# Pod pod0
GPU_DEVICE_0=gpu-0

# Pod pod1
GPU_DEVICE_0=gpu-0
```

## Cleanup

```bash
cd demo/examples/gpu-allow-multiple-allocations && kubectl delete -f gpu-allow-multiple-allocations.yaml
```
