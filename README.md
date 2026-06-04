# Example Resource Driver for Dynamic Resource Allocation (DRA)

This repository contains an example resource driver for use with the [Dynamic
Resource Allocation
(DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
feature of Kubernetes.

It is intended to demonstrate best-practices for how to construct a DRA
resource driver and wrap it in a [helm chart](https://helm.sh/). It can be used
as a starting point for implementing a driver for your own set of resources.

## Quickstart and Demo

Before diving into the details of how this example driver is constructed, it's
useful to run through a quick demo of it in action.

The driver itself provides access to a set of mock GPU devices, and this demo
walks through the process of building and installing the driver followed by
running a set of workloads that consume these GPUs.

The procedure below has been tested and verified on both Linux and Mac.

### Prerequisites

* [GNU Make 3.81+](https://www.gnu.org/software/make/)
* [GNU Tar 1.34+](https://www.gnu.org/software/tar/)
* [docker v20.10+ (including buildx)](https://docs.docker.com/engine/install/) or [Podman v4.9+](https://podman.io/docs/installation)
* [kind v0.32.0+](https://kind.sigs.k8s.io/docs/user/quick-start/) (required for Kubernetes 1.36 node images / containerd config v4; `kind load` fails on older kind)
* [helm v3.7.0+](https://helm.sh/docs/intro/install/)
* [kubectl v1.18+](https://kubernetes.io/docs/reference/kubectl/)

### Creating a cluster and installing the example driver

#### Kind
We start by first cloning this repository and `cd`ing into it. All of the
scripts and example Pod specs used in this demo are contained here, so take a
moment to browse through the various files and see what's available:
```
git clone https://github.com/kubernetes-sigs/dra-example-driver.git
cd dra-example-driver
```

**Note**: The scripts will automatically use either `docker`, or `podman` as the container tool command, whichever
can be found in the PATH. To override this behavior, set `CONTAINER_TOOL` environment variable either by calling
`export CONTAINER_TOOL=docker`, or by prepending `CONTAINER_TOOL=docker` to a script
(e.g. `CONTAINER_TOOL=docker ./path/to/script.sh`). Keep in mind that building Kind images currently requires Docker.

From here we will build the image for the example resource driver:
```bash
./demo/build-driver.sh
```

### Container image make recipes

The image build logic lives in `deployments/container/Makefile`.
If variables are not provided, defaults are:

- `IMAGE_NAME=registry.example.com/dra-example-driver`
- `VERSION=latest`
- `PLATFORMS=<current host platform>` (for example `linux/amd64`, `linux/arm64`, or `linux/ppc64le`) when `PLATFORMS` is **unset**
- `CONTAINER_TOOL=docker`

For demo scripts, `PLATFORMS` is the canonical variable and `DRIVER_IMAGE_PLATFORMS`
is only a backward compatible fallback. Setting both is treated as an error.

`cloudbuild.yaml` and `demo/scripts/push-driver-image.sh` may provide different
`PLATFORMS` defaults depending on the workflow:

1. **If `PLATFORMS` is set (e.g. by `cloudbuild.yaml`)**, the Makefile uses it as-is.
2. **If `PLATFORMS` is unset**, `demo/scripts/push-driver-image.sh` fills a fallback
   (currently `linux/amd64,linux/arm64,linux/ppc64le`) and `deployments/container/Makefile`
   falls back to the host platform.
3. **If `PLATFORMS` is set to an empty string**, `deployments/container/Makefile` fails
   with a clear error (to avoid confusing silent buildx behavior).

- Build a single-arch image with the standard Docker/Podman build flow:
  ```bash
  make -f deployments/container/Makefile build VERSION=<tag> IMAGE_NAME=<name|registry/name> CONTAINER_TOOL=<docker|podman>
  ```
- Build for specific platform(s):
  ```bash
  make -f deployments/container/Makefile build VERSION=<tag> IMAGE_NAME=<name|registry/name> CONTAINER_TOOL=docker PLATFORMS='linux/amd64,linux/arm64'
  ```
- Push for current platform:
  ```bash
  make -f deployments/container/Makefile push VERSION=<tag> IMAGE_NAME=<registry/name> CONTAINER_TOOL=<docker|podman>
  ```
- Push for specific platform(s):
  ```bash
  make -f deployments/container/Makefile push VERSION=<tag> IMAGE_NAME=<registry/name> CONTAINER_TOOL=docker PLATFORMS='linux/amd64,linux/arm64'
  ```

For Docker, `build` with multiple platforms performs a Buildx build without loading an image into the local Docker daemon; use `push` to publish multi-arch images.

#### Multi-platform builds on Linux (amd64)

Building for a platform other than your host CPU (for example `linux/arm64` on an
`x86_64` machine) requires Docker Buildx to **run** container steps for that
architecture during the image build. That needs either native hardware or
userspace emulation via [QEMU and `binfmt_misc`](https://docs.docker.com/build/building/multi-platform/#qemu).

- **Docker Desktop** (macOS/Windows) and many CI images ship with this enabled.
- **Linux on amd64** often does not. The same applies to an explicit single
  platform that does not match the host (for example `PLATFORMS=linux/arm64` on
  x86_64): the Makefile uses buildx for that case. If a build fails with
  `exec format error` on an `linux/arm64` step, install emulation support:

  ```bash
  docker run --privileged --rm tonistiigi/binfmt --install all
  ```

  Then create or bootstrap a buildx builder (`build` and `push` do this
  automatically via `ensure-buildx-builder` for multi-platform or cross-platform
  single-platform builds):

  ```bash
  make -f deployments/container/Makefile ensure-buildx-builder
  ```

  Verify with:

  ```bash
  docker run --rm --platform linux/arm64 alpine uname -m
  ```

  The output should be `aarch64`.

On Apple Silicon, single-arch `linux/arm64` builds work natively; building
`linux/amd64` uses emulation the same way.

And create a `kind` cluster to run it in:
```bash
./demo/clusters/kind/create-cluster.sh
```

Once the cluster has been created successfully, double check everything is
coming up as expected:
```console
$ kubectl get pod -A
NAMESPACE            NAME                                                               READY   STATUS    RESTARTS   AGE
kube-system          coredns-5d78c9869d-6jrx9                                           1/1     Running   0          1m
kube-system          coredns-5d78c9869d-dpr8p                                           1/1     Running   0          1m
kube-system          etcd-dra-example-driver-cluster-control-plane                      1/1     Running   0          1m
kube-system          kindnet-g88bv                                                      1/1     Running   0          1m
kube-system          kindnet-msp95                                                      1/1     Running   0          1m
kube-system          kube-apiserver-dra-example-driver-cluster-control-plane            1/1     Running   0          1m
kube-system          kube-controller-manager-dra-example-driver-cluster-control-plane   1/1     Running   0          1m
kube-system          kube-proxy-kgz4z                                                   1/1     Running   0          1m
kube-system          kube-proxy-x6fnd                                                   1/1     Running   0          1m
kube-system          kube-scheduler-dra-example-driver-cluster-control-plane            1/1     Running   0          1m
local-path-storage   local-path-provisioner-7dbf974f64-9jmc7                            1/1     Running   0          1m
```

The validating admission webhook is disabled by default. To enable it, install cert-manager and its CRDs, then
set the `webhook.enabled=true` value when the dra-example-driver chart is installed.
```bash
helm install \
  --repo https://charts.jetstack.io \
  --version v1.20.2 \
  --create-namespace \
  --namespace cert-manager \
  --wait \
  --set crds.enabled=true \
  cert-manager \
  cert-manager
```
More options for installing cert-manager can be found in [their docs](https://cert-manager.io/docs/installation/)

And then install the example resource driver via `helm`.
```bash
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  dra-example-driver \
  deployments/helm/dra-example-driver
```

Double check the driver components have come up successfully:
```console
$ kubectl get pod -n dra-example-driver
NAME                                                  READY   STATUS    RESTARTS   AGE
dra-example-driver-kubeletplugin-qwmbl                1/1     Running   0          1m
dra-example-driver-webhook-7d465fbd5b-n2wxt           1/1     Running   0          1m
```

And show the initial state of available GPU devices on the worker node:
```
$ kubectl get resourceslice -o yaml
apiVersion: v1
items:
- apiVersion: resource.k8s.io/v1
  kind: ResourceSlice
  metadata:
    creationTimestamp: "2024-12-09T16:17:09Z"
    generateName: dra-example-driver-cluster-worker-gpu.example.com-
    generation: 1
    name: dra-example-driver-cluster-worker-gpu.example.com-rf2f7
    ownerReferences:
    - apiVersion: v1
      controller: true
      kind: Node
      name: dra-example-driver-cluster-worker
      uid: 6633c2e1-d947-40c3-ba1f-78f3c9aad05c
    resourceVersion: "530"
    uid: d13fd8bd-0a71-43e1-ba79-ebd2fae4847a
  spec:
    driver: gpu.example.com
    nodeName: dra-example-driver-cluster-worker
    pool:
      generation: 0
      name: dra-example-driver-cluster-worker
      resourceSliceCount: 1
    devices:
    - attributes:
        driverVersion:
          version: 1.0.0
        index:
          int: 0
        model:
          string: LATEST-GPU-MODEL
        uuid:
          string: gpu-18db0e85-99e9-c746-8531-ffeb86328b39
      capacity:
        memory:
          value: 80Gi
      name: gpu-0
    - attributes:
        driverVersion:
          version: 1.0.0
        index:
          int: 1
        model:
          string: LATEST-GPU-MODEL
        uuid:
          string: gpu-93d37703-997c-c46f-a531-755e3e0dc2ac
      capacity:
        memory:
          value: 80Gi
      name: gpu-1
    - attributes:
        driverVersion:
          version: 1.0.0
        index:
          int: 2
        model:
          string: LATEST-GPU-MODEL
        uuid:
          string: gpu-ee3e4b55-fcda-44b8-0605-64b7a9967744
      capacity:
        memory:
          value: 80Gi
      name: gpu-2
    - attributes:
        driverVersion:
          version: 1.0.0
        index:
          int: 3
        model:
          string: LATEST-GPU-MODEL
        uuid:
          string: gpu-9ede7e32-5825-a11b-fa3d-bab6d47e0243
      capacity:
        memory:
          value: 80Gi
      name: gpu-3
    - attributes:
        driverVersion:
          version: 1.0.0
        index:
          int: 4
        model:
          string: LATEST-GPU-MODEL
        uuid:
          string: gpu-e7b42cb1-4fd8-91b2-bc77-352a0c1f5747
      capacity:
        memory:
          value: 80Gi
      name: gpu-4
    - attributes:
        driverVersion:
          version: 1.0.0
        index:
          int: 5
        model:
          string: LATEST-GPU-MODEL
        uuid:
          string: gpu-f11773a1-5bfb-e48b-3d98-1beb5baaf08e
      capacity:
        memory:
          value: 80Gi
      name: gpu-5
    - attributes:
        driverVersion:
          version: 1.0.0
        index:
          int: 6
        model:
          string: LATEST-GPU-MODEL
        uuid:
          string: gpu-0159f35e-99ee-b2b5-74f1-9d18df3f22ac
      capacity:
        memory:
          value: 80Gi
      name: gpu-6
    - attributes:
        driverVersion:
          version: 1.0.0
        index:
          int: 7
        model:
          string: LATEST-GPU-MODEL
        uuid:
          string: gpu-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b
      capacity:
        memory:
          value: 80Gi
      name: gpu-7
kind: List
metadata:
  resourceVersion: ""
```

#### Other platforms
This demo uses kind by default. Additional platform-specific setup and cleanup
guides are documented in [`demo/clusters`](demo/clusters/README.md), including
GKE instructions in [`demo/clusters/gke`](demo/clusters/gke/README.md).

### Run example workloads (shared across kind and GKE)
Next, deploy four example apps that demonstrate how `ResourceClaim`s,
`ResourceClaimTemplate`s, and custom `GpuConfig` objects can be used to
select and configure resources in various ways:
```bash
kubectl apply --filename=demo/basic-resourceclaimtemplate.yaml \
  --filename=demo/basic-multiple-requests.yaml \
  --filename=demo/basic-shared-claim-across-containers.yaml \
  --filename=demo/basic-shared-claim-across-pods.yaml \
  --filename=demo/basic-resourceclaim-opaque-config.yaml
```

And verify that they are coming up successfully:
```console
$ kubectl get pod -A
NAMESPACE                              NAME   READY   STATUS              RESTARTS   AGE
...
basic-resourceclaimtemplate            pod0   0/1     Pending             0          2s
basic-resourceclaimtemplate            pod1   0/1     Pending             0          2s
basic-multiple-requests                pod0   0/2     Pending             0          2s
basic-shared-claim-across-containers   pod0   0/1     ContainerCreating   0          2s
basic-shared-claim-across-containers   pod1   0/1     ContainerCreating   0          2s
basic-shared-claim-across-pods         pod0   0/1     Pending             0          2s
basic-resourceclaim-opaque-config      pod0   0/4     Pending             0          2s
...
```

Use your favorite editor to look through each of the `basic-*.yaml`
files and see what they are doing.

Then dump the logs of each app to verify that GPUs were allocated to them
according to these semantics:
```bash
for ns in basic-resourceclaimtemplate basic-multiple-requests basic-shared-claim-across-containers basic-shared-claim-across-pods basic-resourceclaim-opaque-config; do \
  echo "${ns}:"
  for pod in $(kubectl get pod -n ${ns} --output=jsonpath='{.items[*].metadata.name}'); do \
    for ctr in $(kubectl get pod -n ${ns} ${pod} -o jsonpath='{.spec.containers[*].name}'); do \
      echo "${pod} ${ctr}:"
      kubectl logs -n ${ns} ${pod} -c ${ctr}| grep -E "GPU_DEVICE_[0-9]+" | grep -v "RESOURCE_CLAIM"
    done
  done
  echo ""
done
```

This should produce output similar to the following:
```bash
basic-resourceclaimtemplate:
pod0 ctr0:
declare -x GPU_DEVICE_6="gpu-6"
pod1 ctr0:
declare -x GPU_DEVICE_7="gpu-7"

basic-multiple-requests:
pod0 ctr0:
declare -x GPU_DEVICE_0="gpu-0"
declare -x GPU_DEVICE_1="gpu-1"

basic-shared-claim-across-containers:
pod0 ctr0:
declare -x GPU_DEVICE_2="gpu-2"
declare -x GPU_DEVICE_2_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_2_TIMESLICE_INTERVAL="Default"
pod0 ctr1:
declare -x GPU_DEVICE_2="gpu-2"
declare -x GPU_DEVICE_2_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_2_TIMESLICE_INTERVAL="Default"

basic-shared-claim-across-pods:
pod0 ctr0:
declare -x GPU_DEVICE_3="gpu-3"
declare -x GPU_DEVICE_3_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_3_TIMESLICE_INTERVAL="Default"
pod1 ctr0:
declare -x GPU_DEVICE_3="gpu-3"
declare -x GPU_DEVICE_3_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_3_TIMESLICE_INTERVAL="Default"

basic-resourceclaim-opaque-config:
pod0 ts-ctr0:
declare -x GPU_DEVICE_4="gpu-4"
declare -x GPU_DEVICE_4_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_4_TIMESLICE_INTERVAL="Long"
pod0 ts-ctr1:
declare -x GPU_DEVICE_4="gpu-4"
declare -x GPU_DEVICE_4_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_4_TIMESLICE_INTERVAL="Long"
pod0 sp-ctr0:
declare -x GPU_DEVICE_5="gpu-5"
declare -x GPU_DEVICE_5_PARTITION_COUNT="10"
declare -x GPU_DEVICE_5_SHARING_STRATEGY="SpacePartitioning"
pod0 sp-ctr1:
declare -x GPU_DEVICE_5="gpu-5"
declare -x GPU_DEVICE_5_PARTITION_COUNT="10"
declare -x GPU_DEVICE_5_SHARING_STRATEGY="SpacePartitioning"
```

In this example resource driver, no "actual" GPUs are made available to any
containers. Instead, a set of environment variables are set in each container
to indicate which GPUs *would* have been injected into them by a real resource
driver and how they *would* have been configured.

You can use the IDs of the GPUs as well as the GPU sharing settings set in
these environment variables to verify that they were handed out in a way
consistent with the semantics shown in the figure above.


### Cleanup

Once you have verified everything is running correctly, delete all of the
example apps:
```bash
kubectl delete --wait=false --filename=demo/basic-resourceclaimtemplate.yaml \
  --filename=demo/basic-multiple-requests.yaml \
  --filename=demo/basic-shared-claim-across-containers.yaml \
  --filename=demo/basic-shared-claim-across-pods.yaml \
  --filename=demo/basic-resourceclaim-opaque-config.yaml
```

And wait for them to terminate:
```console
$ kubectl get pod -A
NAMESPACE                              NAME   READY   STATUS        RESTARTS   AGE
...
basic-resourceclaimtemplate            pod0   1/1     Terminating   0          31m
basic-resourceclaimtemplate            pod1   1/1     Terminating   0          31m
basic-multiple-requests                pod0   2/2     Terminating   0          31m
basic-shared-claim-across-containers   pod0   1/1     Terminating   0          31m
basic-shared-claim-across-containers   pod1   1/1     Terminating   0          31m
basic-shared-claim-across-pods         pod0   1/1     Terminating   0          31m
basic-resourceclaim-opaque-config      pod0   4/4     Terminating   0          31m
...
```

#### Kind
Finally, you can run the following to clean up your environment and delete the
kind cluster started previously:
```bash
./demo/clusters/kind/delete-cluster.sh
```

#### Other platforms
Use the cleanup steps documented in [`demo/clusters`](demo/clusters/README.md).

## Device Profiles

The example driver can manage several different kinds of devices to demonstrate
a variety of DRA features. The functionality for each kind of device is
organized into a "profile." Only one profile is active at a time for a given
instance of the example driver, though the example driver may be installed
multiple times in the same cluster with different active profiles. See the Helm
chart's `deviceProfile` value in values.yaml for available profiles.

For driver developers, this pattern is specific to the example driver and not
intended to be a recommendation for all DRA drivers. Other drivers will likely
be simpler by implementing their logic more directly than through an
abstraction like the example driver's profiles.

### Available profiles

The default profile is `gpu`, which is what the quickstart above installs; all
existing `demo/gpu-test*.yaml` fixtures continue to work unchanged. An
additional profile exists for devices in vfio mode. This is for virtualized workloads like KubeVirt and Kata.

| Profile    | Driver name             | Devices advertise                                                       | Discovery                                          | Demo fixtures              |
|------------|-------------------------|-------------------------------------------------------------------------|----------------------------------------------------|----------------------------|
| `gpu`      | `gpu.example.com`       | model/index/uuid                                                        | Mock (count via `--num-devices`)                   | `demo/gpu-test{1..5}.yaml` |
| `vfio-gpu` | `vfio-gpu.example.com`  | `resource.kubernetes.io/pciBusID`, vendor/device/class, IOMMU group| Real, scans `/sys/bus/pci/drivers/vfio-pci` (vendor/device/class read from `/sys/bus/pci/devices/<BDF>`) | `demo/clusters/kind/vfio-gpu-test.yaml`   |

The `vfio-gpu` profile relies on the upstream kubeletplugin framework's
[KEP-5304][kep-5304] support to write a device metadata file at
`/var/run/kubernetes.io/dra-device-attributes/<claim>/<request>/metadata.json`
inside any consuming pod (enabled via the `kubeletPlugin.enableDeviceMetadata`
Helm value / `--enable-device-metadata` CLI flag).

The profile additionally injects, via the per-claim CDI spec built at
`NodePrepareResources` time, the VFIO character devices the launcher
needs to actually open the device: `/dev/vfio/<iommu_group>` for the
allocated BDF and the userspace `/dev/vfio/vfio` entry point.

The profile discovers devices by walking `/sys/bus/pci/drivers/vfio-pci/`,
so every advertised device is by construction already bound to `vfio-pci`.
No vendor/device filter or CEL selector is needed: the kernel has already
partitioned the bus for us.

Binding devices to `vfio-pci` is the operator's job (kernel cmdline
`vfio-pci.ids=`, `driverctl set-override <BDF> vfio-pci`, a custom systemd
unit, ...). Hosts that haven't bound anything yet will advertise an empty
pool rather than fail the driver startup.

### Installing a non-default profile

```bash
# vfio-gpu profile (real PCI passthrough for KubeVirt VMIs)
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver-vfio \
  --set deviceProfile=vfio-gpu \
  --set kubeletPlugin.enableDeviceMetadata=true \
  --set driverName=vfio-gpu.example.com \
  dra-example-driver-vfio \
  deployments/helm/dra-example-driver
```

Each profile is a separate driver in the cluster, so both can be
installed side-by-side without conflict.

### vfio-gpu kind demo

The default [`demo/clusters/kind/create-cluster.sh`](demo/clusters/kind/create-cluster.sh)
quickstart targets the mock `gpu` profile. For vfio-gpu, prepare a Linux host with
devices bound to `vfio-pci`, then create the cluster with vfio mounts enabled:

```bash
VFIO_GPU=true ./demo/clusters/kind/create-cluster.sh
```

Install the driver with `deviceProfile=vfio-gpu` as above, then apply
[`demo/clusters/kind/vfio-gpu-test.yaml`](demo/clusters/kind/vfio-gpu-test.yaml)
(KubeVirt must be installed separately). See
[`demo/clusters/kind/README.md`](demo/clusters/kind/README.md) for the full walkthrough.

## Anatomy of a DRA resource driver

TBD

## Code Organization

TBD

## Best Practices

TBD

## References

For more information on the DRA Kubernetes feature and developing custom resource drivers, see the following resources:

* [Dynamic Resource Allocation in Kubernetes](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
* TBD

## Community, discussion, contribution, and support

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- [Slack](https://slack.k8s.io/)
- [Mailing List](https://groups.google.com/a/kubernetes.io/g/dev)

### Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](code-of-conduct.md).

[owners]: https://git.k8s.io/community/contributors/guide/owners.md
[Creative Commons 4.0]: https://git.k8s.io/website/LICENSE
