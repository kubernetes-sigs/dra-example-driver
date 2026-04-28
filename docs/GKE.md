# Running the demo on GKE

Use the helper scripts in `demo/gke/` to create or delete a GKE cluster and
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

CDI must be enabled in containerd for the DRA driver to work. In GKE, CDI is
enabled by default in recent versions, and the default script configuration
uses the rapid release channel.

Because DRA is still a beta feature, the required resource APIs must be
explicitly enabled when creating the cluster. The create script handles that by
default.

## Create cluster

```bash
./demo/gke/create-cluster.sh
```

Supported environment variables:

- `GKE_CLUSTER_NAME` (default: `dra-example-driver-cluster`)
- `GKE_LOCATION` (default: `us-central1-c`)
- `GKE_RELEASE_CHANNEL` (default: `rapid`)
- `GKE_NUM_NODES` (default: `1`)
- `GKE_ENABLE_K8S_UNSTABLE_APIS` (default enables required DRA APIs)

## Install driver

```bash
./demo/gke/install-driver.sh
```

Supported environment variables:

- `DRIVER_RELEASE_NAME` (default: `dra-example-driver`)
- `DRIVER_NAMESPACE` (default: `dra-example-driver`)

The install script enables `resourcequota.enabled=true` for GKE.

To avoid chart/image skew, prefer matching source revisions for chart and
image when changing default image tags.

## Uninstall driver

```bash
./demo/gke/uninstall-driver.sh
```

## Delete cluster

```bash
./demo/gke/delete-cluster.sh
```
