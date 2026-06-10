# Partitionable Devices Example

This example demonstrates the **DRAPartitionableDevices** feature, which allows GPUs to be exposed with shared counters enabling flexible partitioning.

- Each physical GPU has a **counter set** (memory and compute)
- Multiple **partition devices** consume from those counters
- A full-GPU device is also available that consumes all counters
- The scheduler uses shared counters to track that partitions share GPU resources

**Setup**: One pod with one container requesting 2 GPU partitions from the same physical GPU.

## Overview

```mermaid
graph TD
    subgraph Node["Node (Physical Hardware)"]
        subgraph GPU1["GPU 1 (Physical)"]
            SC1["Shared Counters<br/>Memory: 100%<br/>Compute: 100%"]
            P1["Partition 1<br/>25% GPU"]
            P2["Partition 2<br/>25% GPU"]
            P3["Partition 3<br/>25% GPU"]
            P4["Partition 4<br/>25% GPU"]
            SC1 -.tracks.-> P1
            SC1 -.tracks.-> P2
            SC1 -.tracks.-> P3
            SC1 -.tracks.-> P4
        end
    end
    
    subgraph Pod["Pod 0"]
        C1["Container: ctr0"]
    end
    
    P1 --- C1
    P2 --- C1
    
    style Node fill:#e3f2fd,stroke:#333,stroke-width:2px,color:#000
    style GPU1 fill:#e8f4f8,stroke:#326ce5,stroke-width:2px,color:#000
    style SC1 fill:#fff3cd,color:#000,stroke:#856404,stroke-width:2px
    style P1 fill:#9b59b6,color:#fff,stroke:#8e44ad,stroke-width:2px
    style P2 fill:#9b59b6,color:#fff,stroke:#8e44ad,stroke-width:2px
    style P3 fill:#95a5a6,color:#fff,stroke:#7f8c8d,stroke-width:2px
    style P4 fill:#95a5a6,color:#fff,stroke:#7f8c8d,stroke-width:2px
    style Pod stroke:#326ce5,stroke-width:2px,color:#fff
    style C1 fill:#d4edda,color:#000,stroke:#28a745,stroke-width:2px
    
    linkStyle 0,1,2,3 stroke:#333,stroke-width:2px
    linkStyle 4,5 stroke:#333,stroke-width:2px
```

## Requirements

### Cluster Requirements
- **Kubernetes 1.35+** with `DRAPartitionableDevices` feature gate enabled
  - Beta in Kubernetes 1.35
  - Enabled by default in Kubernetes 1.36+

### Driver Requirements
- Profile: `gpu`
- Helm values: `kubeletPlugin.gpuPartitions=4`

## Prerequisites

Install the driver with partitioning enabled:

```bash
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  --set kubeletPlugin.gpuPartitions=4 \
  --set kubeletPlugin.numDevices=2 \
  dra-example-driver \
  deployments/helm/dra-example-driver
```
## Verification

### Check ResourceSlices
You should see two slices per node — one for shared counters and one for partition devices:

```bash
kubectl get resourceslices -o wide
```

## Running the Example

Apply the example:

```bash
cd demo/examples/partitionable-devices && kubectl apply -f partitionable-devices.yaml
```

## Expected Output
Check that pod0 gets two GPU partitions:

```bash
kubectl logs -n partitionable-devices pod0 -c ctr0 | grep GPU_DEVICE
```

Expected output should show two GPU partition devices allocated to the container.

## Cleanup

```bash
cd demo/examples/partitionable-devices && kubectl delete -f partitionable-devices.yaml
```
