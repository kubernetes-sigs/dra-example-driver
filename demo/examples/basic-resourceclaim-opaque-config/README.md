# Basic ResourceClaim with Opaque Config Example

## Overview

This example demonstrates advanced GPU configuration using opaque parameters in DRA. It shows how to configure different sharing strategies (TimeSlicing and SpacePartitioning) for different GPUs within the same pod.

**Setup**: One pod with four containers sharing two GPUs, each GPU configured with a different sharing strategy.
- **GPU 1: TimeSlicing Configuration**

   - **Strategy**: TimeSlicing
   - **Interval**: Long
   - **Containers**: ts-ctr0, ts-ctr1
   - **Behavior**: Containers take turns accessing the GPU with long time slices

- **GPU 2: SpacePartitioning Configuration**

   - **Strategy**: SpacePartitioning
   - **Partition Count**: 10
   - **Containers**: sp-ctr0, sp-ctr1
   - **Behavior**: Each container gets dedicated partition(s) of the GPU

## GPU Allocation

```mermaid
graph TD
    subgraph Pod["Pod 1"]
        TS0["Container<br/>ts-ctr0<br/>(TimeSlicing)"]
        TS1["Container<br/>ts-ctr1<br/>(TimeSlicing)"]
        SP0["Container<br/>sp-ctr0<br/>(SpacePartitioning)"]
        SP1["Container<br/>sp-ctr1<br/>(SpacePartitioning)"]
    end

    G1[["GPU 1<br/>TimeSlicing<br/>Interval: Long"]]
    G2[["GPU 2<br/>SpacePartitioning<br/>Partitions:10"]]

    TS0 -.time-shared.-> G1
    TS1 -.time-shared.-> G1
    SP0 --- G2
    SP1 --- G2

    style Pod stroke:#326ce5
    style G1 fill:#9b59b6,color:#fff,stroke:#8e44ad,stroke-width:3px
    style G2 fill:#3498db,color:#fff,stroke:#2980b9,stroke-width:3px
    style TS0 fill:#e8daef,color:#000,stroke:#8e44ad,stroke-width:2px
    style TS1 fill:#e8daef,color:#000,stroke:#8e44ad,stroke-width:2px
    style SP0 fill:#d6eaf8,color:#000,stroke:#2980b9,stroke-width:2px
    style SP1 fill:#d6eaf8,color:#000,stroke:#2980b9,stroke-width:2px
```

## Requirements

### Driver Requirements

- **Profile**: gpu
- **GPUs**: 2 (minimum)

### Cluster Requirements

- Kubernetes 1.34+

## How to Run

1. Apply the example:

   ```bash
   cd demo/examples/basic-resourceclaim-opaque-config && kubectl apply -f basic-resourceclaim-opaque-config.yaml
   ```

2. Verify the pod is running:

   ```bash
   kubectl get pods -n basic-resourceclaim-opaque-config
   ```

3. Check GPU allocation and configuration for TimeSlicing containers:

   ```bash
   kubectl logs -n basic-resourceclaim-opaque-config pod0 -c ts-ctr0 | grep -E "GPU_DEVICE|SHARING_STRATEGY|TIMESLICE_INTERVAL"
   kubectl logs -n basic-resourceclaim-opaque-config pod0 -c ts-ctr1 | grep -E "GPU_DEVICE|SHARING_STRATEGY|TIMESLICE_INTERVAL"
   ```

4. Check GPU allocation and configuration for SpacePartitioning containers:
   ```bash
   kubectl logs -n basic-resourceclaim-opaque-config pod0 -c sp-ctr0 | grep -E "GPU_DEVICE|SHARING_STRATEGY|PARTITION_COUNT"
   kubectl logs -n basic-resourceclaim-opaque-config pod0 -c sp-ctr1 | grep -E "GPU_DEVICE|SHARING_STRATEGY|PARTITION_COUNT"
   ```

## Expected Output

### TimeSlicing Containers (ts-ctr0 and ts-ctr1)

Both containers should show:

```
GPU_DEVICE_0=gpu-X
SHARING_STRATEGY=TimeSlicing
TIMESLICE_INTERVAL=Long
```

- Both containers share the **same GPU ID**
- They take turns using the GPU with long time intervals

### SpacePartitioning Containers (sp-ctr0 and sp-ctr1)

Both containers should show:

```
GPU_DEVICE_0=gpu-Y
SHARING_STRATEGY=SpacePartitioning
PARTITION_COUNT=10
```

- Both containers share the **same GPU ID** (different from TimeSlicing GPU)
- Each container gets dedicated partition(s) from the 10 available partitions

## Cleanup

```bash
cd demo/examples/basic-resourceclaim-opaque-config && kubectl delete -f basic-resourceclaim-opaque-config.yaml
```
