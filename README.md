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
* [kind v0.17.0+](https://kind.sigs.k8s.io/docs/user/quick-start/)
* [helm v3.7.0+](https://helm.sh/docs/intro/install/)
* [kubectl v1.18+](https://kubernetes.io/docs/reference/kubectl/)

### Demo
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

And create a `kind` cluster to run it in:
```bash
./demo/create-cluster.sh
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
  --version v1.16.3 \
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
```bash
kubectl get resourceslice -o yaml
```

You should see 2 GPUs (gpu-0, gpu-1) on the worker node, each with model
`LATEST-GPU-MODEL` and 80Gi of memory.

Next, deploy some example apps to see DRA in action. The default configuration
provides 2 GPUs per node, which is enough to run each example individually.
Each example file has detailed comments at the top explaining what it
demonstrates and how to verify the results.

**Example 1: Exclusive GPU access**

Two pods each requesting their own distinct GPU:
```bash
kubectl apply -f demo/two-pods-one-gpu-each.yaml
kubectl wait --for=condition=Ready pod/pod0 pod/pod1 -n two-pods-one-gpu-each --timeout=60s
```

Check that each pod got a different GPU:
```bash
for pod in pod0 pod1; do
  echo "${pod}:"
  kubectl logs -n two-pods-one-gpu-each ${pod} -c ctr0 | grep -E "GPU_DEVICE_[0-9]+=" | grep -v "RESOURCE_CLAIM"
done
```

Clean up before the next example:
```bash
kubectl delete -f demo/two-pods-one-gpu-each.yaml
```

**Example 2: Shared GPU across containers**

Two containers in one pod sharing a single GPU:
```bash
kubectl apply -f demo/shared-gpu-across-containers.yaml
kubectl wait --for=condition=Ready pod/pod0 -n shared-gpu-across-containers --timeout=60s
```

Check that both containers see the same GPU with TimeSlicing:
```bash
for ctr in ctr0 ctr1; do
  echo "pod0 ${ctr}:"
  kubectl logs -n shared-gpu-across-containers pod0 -c ${ctr} | grep -E "GPU_DEVICE_[0-9]+" | grep -v "RESOURCE_CLAIM"
done
```

Clean up before the next example:
```bash
kubectl delete -f demo/shared-gpu-across-containers.yaml
```

**Example 3: GPU sharing strategies**

Two GPUs configured with different sharing modes (TimeSlicing and SpacePartitioning):
```bash
kubectl apply -f demo/gpu-sharing-strategies.yaml
kubectl wait --for=condition=Ready pod/pod0 -n gpu-sharing-strategies --timeout=60s
```

Check that ts-ctr0/ts-ctr1 share one GPU with TimeSlicing and sp-ctr0/sp-ctr1
share another with SpacePartitioning:
```bash
for ctr in ts-ctr0 ts-ctr1 sp-ctr0 sp-ctr1; do
  echo "pod0 ${ctr}:"
  kubectl logs -n gpu-sharing-strategies pod0 -c ${ctr} | grep -E "GPU_DEVICE_[0-9]+" | grep -v "RESOURCE_CLAIM"
done
```

Clean up:
```bash
kubectl delete -f demo/gpu-sharing-strategies.yaml
```

In this example resource driver, no "actual" GPUs are made available to any
containers. Instead, a set of environment variables are set in each container
to indicate which GPUs *would* have been injected into them by a real resource
driver and how they *would* have been configured.

For the full list of all 8 available examples, see [`demo/README.md`](demo/README.md).
To run multiple examples at the same time, increase `kubeletPlugin.numDevices`
when installing the Helm chart.

### Demo DRA Admin Access Feature
This example driver includes support for the [DRA AdminAccess feature](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/#admin-access), which allows administrators to gain privileged access to devices already in use by other users. This example demonstrates the end-to-end flow by setting the `DRA_ADMIN_ACCESS` environment variable. A driver managing real devices could use this to expose host hardware information.

See [`demo/admin-access.yaml`](demo/admin-access.yaml) for the complete example and inline documentation. Key points:

1. **Namespace**: Must have the `resource.kubernetes.io/admin-access` label set to create ResourceClaimTemplate and ResourceClaim with `adminAccess: true` for Kubernetes v1.34+.
2. **Resource Claim Template**: Request must have `adminAccess: true`. The `allocationMode: All` is used to demonstrate accessing all available devices with admin privileges.
3. **Container**: Will receive elevated privileges from the driver, represented here as environment variables (e.g., `DRA_ADMIN_ACCESS=true`).

To run this demo:
```bash
./demo/test-admin-access.sh
```

### Clean Up

Once you are done, delete the `kind` cluster:
```bash
./demo/delete-cluster.sh
```

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
