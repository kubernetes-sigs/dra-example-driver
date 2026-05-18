# Prioritized Alternatives Example

## Overview

This example demonstrates prioritized alternatives in device requests
([KEP-4816](https://github.com/kubernetes/enhancements/issues/4816)). A single
`DeviceRequest` lists several ways to satisfy itself in priority order via the
`firstAvailable` field. The scheduler tries each subrequest in turn and
allocates the first one that can be satisfied, letting a workload prefer a
high-end device but still run on a less specialized one when the preferred
options are unavailable.

**Setup**: Two pods, each making a single `gpu` request that offers a prioritized
list of alternatives (subrequests), not separate GPUs.

- **pod0 (fallback)**: the first two subrequests carry a CEL selector that no
  device can satisfy, so the request falls through to the third.
- **pod1 (preference)**: both subrequests can be satisfied, so the scheduler
  picks the higher-priority one.

## GPU Allocation

```mermaid
graph LR
    subgraph Pod0 [Pod 0: fallback]
        C0(Container ctr0)
    end
    subgraph FA0 ["gpu (firstAvailable)"]
        A1["1. bleeding-edge-gpu<br/>model == BLEEDING-EDGE-GPU<br/>(no match)"]
        A2["2. huge-gpu<br/>memory >= 1Ti<br/>(no match)"]
        A3["3. older-gpu<br/>any GPU"]
        A1 --> A2 --> A3
    end
    C0 --> A1
    A3 ==>|allocated| G0[[GPU]]

    subgraph Pod1 [Pod 1: preference]
        C1(Container ctr0)
    end
    subgraph FA1 ["gpu (firstAvailable)"]
        B1["1. latest-gpu<br/>model == LATEST-GPU-MODEL<br/>(match)"]
        B2["2. older-gpu<br/>any GPU"]
        B1 --> B2
    end
    C1 --> B1
    B1 ==>|allocated| G1[[GPU]]

    style Pod0 stroke:#326ce5,stroke-width:2px
    style Pod1 stroke:#326ce5,stroke-width:2px
    style C0 fill:#d4edda,color:#000,stroke:#28a745,stroke-width:2px
    style C1 fill:#d4edda,color:#000,stroke:#28a745,stroke-width:2px
    style FA0 fill:#f8f9fa,color:#000,stroke:#6c757d,stroke-width:2px
    style FA1 fill:#f8f9fa,color:#000,stroke:#6c757d,stroke-width:2px
    style A1 fill:#e9ecef,color:#000,stroke:#adb5bd,stroke-width:2px
    style A2 fill:#e9ecef,color:#000,stroke:#adb5bd,stroke-width:2px
    style A3 fill:#9b59b6,color:#fff,stroke:#8e44ad,stroke-width:3px
    style B1 fill:#9b59b6,color:#fff,stroke:#8e44ad,stroke-width:3px
    style B2 fill:#e9ecef,color:#000,stroke:#adb5bd,stroke-width:2px
    style G0 fill:#3498db,color:#fff,stroke:#2980b9,stroke-width:3px
    style G1 fill:#3498db,color:#fff,stroke:#2980b9,stroke-width:3px
```

## How It Works

Each subrequest is matched against the GPU devices that the example driver
advertises. The driver
([`internal/profiles/gpu/gpu.go`](../../../internal/profiles/gpu/gpu.go))
publishes every GPU with a `model` attribute of `LATEST-GPU-MODEL` and a `memory`
capacity of `80Gi`. The subrequests filter on those values using CEL selectors,
and the scheduler uses the first subrequest that can be satisfied.

**pod0 — fallback.** The first two selectors match no device, so `firstAvailable`
falls through to `older-gpu`:

| # | Subrequest | Selector | Devices matched | Result |
| - | ---------- | -------- | --------------- | ------ |
| 1 | `bleeding-edge-gpu` | `model == 'BLEEDING-EDGE-GPU'` | 0 (every GPU is `LATEST-GPU-MODEL`) | skipped |
| 2 | `huge-gpu` | `memory >= 1Ti` | 0 (every GPU is `80Gi`) | skipped |
| 3 | `older-gpu` | none | any GPU | **allocated** |

**pod1 — preference.** Both subrequests can be satisfied, so the higher-priority
one wins even though the lower-priority one would also work:

| # | Subrequest | Selector | Devices matched | Result |
| - | ---------- | -------- | --------------- | ------ |
| 1 | `latest-gpu` | `model == 'LATEST-GPU-MODEL'` | any GPU | **allocated** |
| 2 | `older-gpu` | none | any GPU | not reached |

The selectors compare against attributes and capacities the driver advertises,
not anything defined in the claim. A subrequest whose selector matches zero
devices cannot be satisfied, so the scheduler moves on to the next one in the
list.

## Requirements

### Driver Requirements

- **Profile**: gpu
- **GPUs**: 2 (one per pod)

### Cluster Requirements

- **Kubernetes 1.34+** with the `DRAPrioritizedList` feature gate enabled
  - Beta and enabled by default in Kubernetes 1.34–1.35
  - GA in Kubernetes 1.36+

## How to Run

1. Apply the example:

   ```bash
   cd demo/examples/prioritized-alternatives && kubectl apply -f prioritized-alternatives.yaml
   ```

2. Verify the pods are running:

   ```bash
   kubectl get pods -n prioritized-alternatives
   ```

3. Check a GPU was injected into each container:

   ```bash
   kubectl logs -n prioritized-alternatives pod0 -c ctr0 | grep GPU_DEVICE
   kubectl logs -n prioritized-alternatives pod1 -c ctr0 | grep GPU_DEVICE
   ```

4. Check which subrequest the scheduler chose for each pod:

   ```bash
   kubectl get resourceclaim -n prioritized-alternatives \
     -o jsonpath='{range .items[*]}{.status.allocation.devices.results[*].request}{"\n"}{end}'
   ```

## Expected Output

- **Pod Status**: Both pods should be running successfully.
- **GPU Allocation**: Each container has exactly one `GPU_DEVICE` environment
  variable, and the two pods get distinct GPUs.
- **Chosen Subrequest**: The allocation results reference the chosen subrequest
  using the `<main request>/<subrequest>` format:

  ```
  gpu/older-gpu     # pod0 fell back to the only satisfiable subrequest
  gpu/latest-gpu    # pod1 preferred the higher-priority subrequest
  ```

## Cleanup

```bash
cd demo/examples/prioritized-alternatives && kubectl delete -f prioritized-alternatives.yaml
```
