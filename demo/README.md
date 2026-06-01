# Demo Examples

This directory contains example workloads that demonstrate different ways to
request and configure devices using Dynamic Resource Allocation (DRA).

Examples prefixed with `basic-` are a good starting point for
learning about DRA.

Each example file has detailed comments at the top explaining what it
demonstrates, what output to expect, and the driver and cluster requirements.

## Running Examples

Each example can be run individually:

```bash
kubectl apply -f demo/<example-name>.yaml
```

To clean up:

```bash
kubectl delete -f demo/<example-name>.yaml
```

## Scripts

### install-kubevirt-daily.sh

The `scripts/install-kubevirt-daily.sh` script installs the latest nightly build of KubeVirt on your Kubernetes cluster and enables DRA (Dynamic Resource Allocation) feature gates.

**Usage:**

```bash
./demo/scripts/install-kubevirt-daily.sh
```

**Requirements:**

- `kubectl` configured to access your cluster
- Sufficient cluster resources to run KubeVirt
- For minikube or kind clusters, you may need to increase `fs.inotify` limits (see [kubernetes/minikube#18831](https://github.com/kubernetes/minikube/issues/18831))

## Notes

- The default Helm chart configures **8 GPUs** per node, which is enough to run
  several examples simultaneously.
- Each example creates its own namespace, so examples don't interfere with
  each other's resource names.
