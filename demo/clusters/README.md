# Cluster lifecycle

This directory holds platform-specific scripts and documentation for bringing
up Kubernetes clusters with Dynamic Resource Allocation (DRA) enabled, for
use with the demo in this repository.

Each subdirectory is named for its platform. Where applicable, scripts follow a
common layout:

- `create-cluster.sh` — create a cluster configured for the demo
- `delete-cluster.sh` — delete that cluster

Platforms may add other scripts or notes next to these entrypoints as needed.

## Available platforms

| Path | Purpose |
|---|---|
| [`kind/`](kind/) | kind cluster for the default `gpu` (mock devices) DRA profile and, with `VFIO_GPU=true`, the `vfio-gpu` profile (host vfio-pci bindings + PCI sysfs mounts). See [`kind/README.md`](kind/README.md). |
| [`gke/`](gke/) | GKE cluster scripts |
