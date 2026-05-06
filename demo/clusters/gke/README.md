# Running the demo on GKE

Use the helper scripts in this directory to create or delete a GKE cluster and
install/uninstall the driver.

## Prerequisites

- [gcloud CLI](https://cloud.google.com/sdk/docs/install)
- [helm](https://helm.sh/docs/intro/install/)
- [kubectl](https://kubernetes.io/docs/reference/kubectl/)
- Authenticated GCP account and a selected project

This flow uses the pre-built image for the kubelet plugin, so no local image
build is required.

To keep the walkthrough simple and close to the kind demo, use a single-node
GKE cluster.

## Create cluster

```bash
./demo/clusters/gke/create-cluster.sh
```

Supported environment variables:

- `GKE_CLUSTER_NAME` (default: `dra-example-driver-cluster`)
- `GKE_LOCATION` (default: `us-central1-c`)
- `GKE_RELEASE_CHANNEL` (default: `rapid`)
- `GKE_NUM_NODES` (default: `1`)

## Install driver

```bash
./demo/clusters/gke/install-driver.sh
```

Supported environment variables:

- `DRIVER_RELEASE_NAME` (default: `dra-example-driver`)
- `DRIVER_NAMESPACE` (default: `dra-example-driver`)

The install script enables `resourceQuota.enabled=true` for GKE.

## Uninstall driver

```bash
./demo/clusters/gke/uninstall-driver.sh
```

## Delete cluster

```bash
./demo/clusters/gke/delete-cluster.sh
```
