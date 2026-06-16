# Demo Examples

The `demo/examples/` directory contains example workloads that demonstrate different ways to request and configure devices using Dynamic Resource Allocation (DRA).

Examples prefixed with `basic-` are a good starting point for
learning about DRA.

Each example directory has a README.md explaining what it demonstrates, what output to expect, and the driver and cluster requirements.

## Running Examples

Each example can be run individually:

```bash
kubectl apply -f demo/examples/<example-name>/<example-name>.yaml
```

To clean up:

```bash
kubectl delete -f demo/examples/<example-name>/<example-name>.yaml
```

## Notes

- The default Helm chart configures **8 GPUs** per node, which is enough to run
  several examples simultaneously.
- Each example creates its own namespace, so examples don't interfere with
  each other's resource names.
