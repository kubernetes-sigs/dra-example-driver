# Running the demo on GKE

Use the helper scripts in this directory to create or delete a GKE cluster and
install/uninstall the driver.

## Prerequisites

- [gcloud CLI](https://cloud.google.com/sdk/docs/install)
- [helm](https://helm.sh/docs/intro/install/)
- [kubectl](https://kubernetes.io/docs/reference/kubectl/)
- Authenticated GCP account and a selected project

This flow installs published release artifacts from `registry.k8s.io` and pins
both the Helm chart and driver image to the same version to avoid chart/image
skew.

The steps below were validated with:
- GKE: `v1.35.2-gke.1485000`
- Driver/chart release: `0.2.0`

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
- `DRIVER_VERSION` (default: `0.2.0`)
- `DRIVER_CHART_REF` (default: `oci://registry.k8s.io/dra-example-driver/charts/dra-example-driver`)

The install script enables `resourceQuota.enabled=true` for GKE and sets
`image.tag` to `DRIVER_VERSION` so chart/image versions remain aligned.

## Uninstall driver

```bash
./demo/clusters/gke/uninstall-driver.sh
```

## Delete cluster

```bash
./demo/clusters/gke/delete-cluster.sh
```
