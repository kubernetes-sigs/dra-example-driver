# ResourceClaimTemplate Device Metadata Example

## Overview

This example demonstrates DRA device metadata with a ResourceClaimTemplate: a pod requests one GPU, and the driver mounts device metadata into the workload container.

**Setup**: One pod with one container requesting 1 GPU via ResourceClaimTemplate, with device metadata enabled on the driver.

## Requirements

### Driver Requirements

- **Profile**: gpu
- **GPUs**: 1
- **DeviceMetadata**: enabled

### Cluster Requirements

- Kubernetes 1.34+

## How to Run

1. Apply the example:

   ```bash
   cd demo/examples/basic-rct-device-metadata && kubectl apply -f basic-rct-device-metadata.yaml
   ```

2. Verify the pod is running:

   ```bash
   kubectl get pods -n basic-rct-device-metadata
   ```

3. Check GPU allocation and device metadata:

   ```bash
   kubectl logs -n basic-rct-device-metadata pod0 -c ctr0 | grep GPU_DEVICE
   kubectl exec -n basic-rct-device-metadata pod0 -c ctr0 -- ls /var/run/dra-example-driver
   ```

## Expected Output

The container should have 1 `GPU_DEVICE` environment variable, and a DRA device metadata file mounted under `/var/run/dra-example-driver`.

## Cleanup

```bash
cd demo/examples/basic-rct-device-metadata && kubectl delete -f basic-rct-device-metadata.yaml
```
