# Running the demo on Minikube

Use the helper scripts in this directory to create or delete a minikube cluster.

## Prerequisites

- [minikube v1.33.0+](https://minikube.sigs.k8s.io/docs/start/)
- [docker v20.10+](https://docs.docker.com/engine/install/) (used as the minikube driver)
- [helm v3.7.0+](https://helm.sh/docs/intro/install/)
- [kubectl v1.18+](https://kubernetes.io/docs/reference/kubectl/)

The steps below were validated with:
- minikube: `v1.36.0`
- Kubernetes: `v1.32.0`
- Driver/chart release: `0.3.0`

## Create cluster

```bash
./demo/clusters/minikube/create-cluster.sh
```

Supported environment variables:

- `MINIKUBE_CLUSTER_NAME` (default: `dra-example-driver-cluster`)
- `MINIKUBE_DRIVER` (default: auto-detected — `docker` if available, otherwise `podman`)

The script starts a minikube cluster with:
- `containerd` as the container runtime (required for CDI support)
- the auto-detected container tool (`docker` or `podman`) as the minikube driver
- Kubernetes version pinned to match `KIND_K8S_TAG` from `demo/scripts/common.sh`
- 4 CPUs (required for the native-resource-request e2e test which requests 2 CPUs)
- `flannel` as the CNI plugin
- the same DRA feature gates as the Kind cluster configuration
- CDI enabled in the containerd configuration

## Build and load the driver image

Build the driver image locally and load it into the minikube cluster:

```bash
./demo/build-driver.sh
```

> **Note**: `demo/build-driver.sh` only auto-loads into a kind cluster. For
> minikube, the `create-cluster.sh` script above handles the initial load. To
> reload after a rebuild, run:
>
> ```bash
> ./demo/scripts/load-driver-image-into-minikube.sh
> ```

## Install driver

```bash
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  dra-example-driver \
  deployments/helm/dra-example-driver
```

## Delete cluster

```bash
./demo/clusters/minikube/delete-cluster.sh
```
