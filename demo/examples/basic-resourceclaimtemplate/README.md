# Basic ResourceClaimTemplate Example

## Overview

This example demonstrates the simplest use case of Dynamic Resource Allocation (DRA): two pods, each requesting one GPU through a ResourceClaimTemplate.

**Setup**: Two pods, each with one container requesting 1 distinct GPU.

## GPU Allocation

```mermaid
graph TD
    subgraph Pod1 [Pod 1]
        C1(Container)
    end
    subgraph Pod2 [Pod 2]
        C2(Container)
    end
    G1[["GPU 1"]]
    G2[["GPU 2"]]
    C1 --- G1
    C2 --- G2

    style Pod1 stroke:#326ce5,stroke-width:2px
    style Pod2 stroke:#326ce5,stroke-width:2px
    style C1 fill:#d4edda,stroke:#28a745,stroke-width:2px
    style C2 fill:#d4edda,stroke:#28a745,stroke-width:2px
    style G1 fill:#9b59b6,color:#fff,stroke:#8e44ad,stroke-width:3px
    style G2 fill:#3498db,color:#fff,stroke:#2980b9,stroke-width:3px

```

## Requirements

### Driver Requirements

- **Profile**: gpu
- **GPUs**: 2

### Cluster Requirements

- Kubernetes 1.34+

## How to Run

1. Apply the example:

   ```bash
   cd demo/examples/basic-resourceclaimtemplate && kubectl apply -f basic-resourceclaimtemplate.yaml
   ```

2. Verify the pods are running:

   ```bash
   kubectl get pods -n basic-resourceclaimtemplate
   ```

3. Check GPU allocation for each pod:
   ```bash
   kubectl logs -n basic-resourceclaimtemplate pod0 -c ctr0 | grep GPU_DEVICE
   kubectl logs -n basic-resourceclaimtemplate pod1 -c ctr0 | grep GPU_DEVICE
   ```

## Expected Output

Each container should have 1 `GPU_DEVICE` environment variable with a distinct GPU ID, confirming that each pod received a different GPU.

## Cleanup

```bash
cd demo/examples/basic-resourceclaimtemplate && kubectl delete -f basic-resourceclaimtemplate.yaml
```
