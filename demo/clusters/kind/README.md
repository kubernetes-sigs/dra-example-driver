# kind cluster

Scripts to create and delete a kind cluster for the DRA demo.

| File | Purpose |
| --- | --- |
| `create-cluster.sh` | Create the cluster (default `gpu` profile demo) |
| `delete-cluster.sh` | Delete the cluster |
| `kind-cluster-config-vfio.yaml` | kind config used when `VFIO_GPU=true` (PCI sysfs + `/dev/vfio` mounts) |
| `vfio-gpu-test.yaml` | ResourceClaimTemplate for the `vfio-gpu` profile |

Shared vfio helpers live in [`demo/scripts/vfio-kind.sh`](../../scripts/vfio-kind.sh).

## Default (mock GPU profile)

```bash
./demo/build-driver.sh
./demo/clusters/kind/create-cluster.sh
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  dra-example-driver \
  deployments/helm/dra-example-driver
```

Uses the CDI-enabled kind node image built by `demo/scripts/build-kind-image.sh` and
`demo/scripts/kind-cluster-config.yaml`.

## vfio-gpu profile (`VFIO_GPU=true`)

**Off by default.** Set `VFIO_GPU=true` when creating the cluster to bind-mount host
PCI sysfs and `/dev/vfio` into kind nodes. This is required for the `vfio-gpu` driver
profile but does **not** install the driver — you still set `deviceProfile=vfio-gpu` in
Helm separately.

**Linux host only.** The host must already have devices bound to `vfio-pci` before
cluster creation. The script verifies bindings and exits if none are found.

### Host setup

Synthetic devices for testing come from [`demo/vfio-gpu`](../../vfio-gpu/README.md):

```bash
sudo FAKE_PCI_DEVICES=8 bash demo/vfio-gpu/setup-host-vfio-pci.sh
ls /sys/bus/pci/drivers/vfio-pci/   # expect BDF entries
```

Run that from the repo root. Real hardware bound to vfio-pci works equally well.

Add `VFIO_SETUP_HOST=true` to let `create-cluster.sh` run that step itself when no
devices are bound. It is off by default because it inserts out-of-tree modules into
the running kernel and because an unbound host may just be one whose real devices
have not been bound yet. With it set, `delete-cluster.sh` unloads them again, so
pass the same variables to both:

```bash
VFIO_GPU=true VFIO_SETUP_HOST=true ./demo/clusters/kind/create-cluster.sh
VFIO_GPU=true VFIO_SETUP_HOST=true ./demo/clusters/kind/delete-cluster.sh
```

### Cluster + driver

```bash
# 1. Cluster (vfio mounts into nodes)
VFIO_GPU=true ./demo/clusters/kind/create-cluster.sh

# 2. Driver image (build from this repo; published images may predate vfio-gpu)
./demo/build-driver.sh

# 3. Driver install (vfio-gpu profile)
helm upgrade --install \
    --create-namespace \
    --namespace dra-example-driver-vfio \
    --set deviceProfile=vfio-gpu \
    --set kubeletPlugin.enableDeviceMetadata=true \
    --set driverName=vfio-gpu.example.com \
    dra-example-driver-vfio \
    deployments/helm/dra-example-driver

Verify the driver:

```bash
kubectl -n dra-example-driver-vfio get ds dra-example-driver-vfio-kubeletplugin \
  -o jsonpath='{.spec.template.spec.containers[?(@.name=="plugin")].env[?(@.name=="DEVICE_PROFILE")].value}{"\n"}'
kubectl get resourceslices -o custom-columns='NAME:.metadata.name,DRIVER:.spec.driver,NODE:.spec.nodeName'
```

Tear down:

```bash
./demo/clusters/kind/delete-cluster.sh
```

### Environment variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `VFIO_GPU` | `false` | Enable vfio-gpu cluster mode (`true` / `1` / `yes` / `on`) |
| `VFIO_SETUP_HOST` | `false` | Build, load, and bind the synthetic PCI devices automatically when none are bound. Needs root |
| `FAKE_PCI_DEVICES` | `8` | Number of synthetic devices to create when `VFIO_SETUP_HOST` is set |
| `VFIO_KIND_NODE_IMAGE` | pinned `kindest/node:v1.35.0` | kind node image when `VFIO_GPU=true` |
| `VFIO_KIND_CLUSTER_CONFIG_PATH` | `kind-cluster-config-vfio.yaml` in this directory | kind config when `VFIO_GPU=true` |

Other variables (`KIND_CLUSTER_NAME`, `CONTAINER_TOOL`, …) come from [`demo/scripts/common.sh`](../../scripts/common.sh).

### Two knobs (cluster vs driver)

| Setting | Layer | What it does |
| --- | --- | --- |
| `VFIO_GPU=true` | Cluster (`create-cluster.sh`) | vfio-pci preflight, vfio kind config, post-create node setup |
| `deviceProfile=vfio-gpu` | Driver (Helm) | Driver discovers vfio-bound PCI devices and prepares `/dev/vfio` CDI |

Both are required for the end-to-end vfio-gpu demo.
