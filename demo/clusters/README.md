# Cluster lifecycle

This directory holds platform-specific scripts and documentation for bringing
up Kubernetes clusters with Dynamic Resource Allocation (DRA) enabled, for
use with the demo in this repository.

Each subdirectory is named for its platform. Where applicable, scripts follow a
common layout:

- `create-cluster.sh` — create a cluster configured for the demo
- `delete-cluster.sh` — delete that cluster

Platforms may add other scripts or notes next to these entrypoints as needed.

## Supported platforms

| Platform | Directory | Notes |
|----------|-----------|-------|
| [kind](https://kind.sigs.k8s.io/) | [`kind/`](kind/) | Default for local development |
| [Minikube](https://minikube.sigs.k8s.io/) | [`minikube/`](minikube/README.md) | Local alternative to kind |
| [GKE](https://cloud.google.com/kubernetes-engine) | [`gke/`](gke/README.md) | Managed cloud cluster |
