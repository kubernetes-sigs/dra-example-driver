# Demo Examples

This directory contains example workloads that demonstrate different ways to
request and configure GPU devices using Dynamic Resource Allocation (DRA).

## Quick Start

The following three examples are featured in the [main README walkthrough](../README.md)
and are designed to run together with the default cluster configuration (2 GPUs):

| Example | Description | GPUs |
|---|---|---|
| [two-pods-one-gpu-each.yaml](two-pods-one-gpu-each.yaml) | Two pods each get their own exclusive GPU | 2 |
| [shared-gpu-across-containers.yaml](shared-gpu-across-containers.yaml) | Two containers in one pod share a single GPU | 1 |
| [gpu-sharing-strategies.yaml](gpu-sharing-strategies.yaml) | TimeSlicing and SpacePartitioning on two GPUs | 2 |

## All Examples

| Example | Description | GPUs | Key Concept |
|---|---|---|---|
| [two-pods-one-gpu-each.yaml](two-pods-one-gpu-each.yaml) | Two pods, each requesting one exclusive GPU | 2 | ResourceClaimTemplate basics |
| [one-pod-two-gpus.yaml](one-pod-two-gpus.yaml) | One container requesting two distinct GPUs | 2 | Multiple requests in a claim |
| [shared-gpu-across-containers.yaml](shared-gpu-across-containers.yaml) | Two containers sharing one GPU within a pod | 1 | Intra-pod GPU sharing |
| [shared-global-claim.yaml](shared-global-claim.yaml) | Two pods sharing a GPU via a pre-created ResourceClaim | 1 | ResourceClaim vs ResourceClaimTemplate |
| [gpu-sharing-strategies.yaml](gpu-sharing-strategies.yaml) | TimeSlicing and SpacePartitioning configuration | 2 | Opaque driver config (GpuConfig) |
| [initcontainer-shared-gpu.yaml](initcontainer-shared-gpu.yaml) | initContainer and container sharing a GPU | 1 | initContainer support |
| [admin-access.yaml](admin-access.yaml) | Admin access to all GPUs with elevated privileges | All | DRA AdminAccess feature |
| [cel-selector.yaml](cel-selector.yaml) | Selecting a GPU by model and memory using CEL | 1 | CEL expression selectors |

## Running Examples

Each example can be run individually:

```bash
kubectl apply -f demo/<example-name>.yaml
```

To clean up:

```bash
kubectl delete -f demo/<example-name>.yaml
```

## Notes

- The default Helm chart configures **2 GPUs** per node, which is enough to run
  any single example (except `admin-access.yaml` which uses all available GPUs).
- To run multiple examples simultaneously, increase `kubeletPlugin.numDevices`
  in the Helm values.
- Each example creates its own namespace, so examples don't interfere with
  each other's resource names.
