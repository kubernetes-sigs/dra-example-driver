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
* [docker v20.10+ (including buildx)](https://docs.docker.com/engine/install/)
* [kind v0.17.0+](https://kind.sigs.k8s.io/docs/user/quick-start/)
* [helm v3.7.0+](https://helm.sh/docs/intro/install/)
* [kubectl v1.18+](https://kubernetes.io/docs/reference/kubectl/)

### Demo
We start by first cloning this repository and `cd`ing into its `demo`
subdirectory. All of the scripts and example Pod specs used in this demo are
contained here, so take a moment to browse through the various files and see
what's available:
```
git clone https://github.com/kubernetes-sigs/dra-example-driver.git
cd dra-example-driver/demo
```

From here we will build the image for the example resource driver:
```bash
./build-driver.sh
```

And create a `kind` cluster to run it in:
```bash
./create-cluster.sh
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

And then install the example resource driver via `helm`:
```bash
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  dra-example-driver \
  ../deployments/helm/dra-example-driver
```

Double check the driver components have come up successfully:
```console
$ kubectl get pod -n dra-example-driver
NAME                                             READY   STATUS    RESTARTS   AGE
dra-example-driver-controller-7555d488db-nbd52   1/1     Running   0          1m
dra-example-driver-kubeletplugin-qwmbl           1/1     Running   0          1m
```

And show the initial state of available GPU devices on the worker node:
```console
$ kubectl describe -n dra-example-driver nas/dra-example-driver-cluster-worker
...
Spec:
  Allocatable Devices:
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-9ede7e32-5825-a11b-fa3d-bab6d47e0243
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-e7b42cb1-4fd8-91b2-bc77-352a0c1f5747
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-f11773a1-5bfb-e48b-3d98-1beb5baaf08e
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-0159f35e-99ee-b2b5-74f1-9d18df3f22ac
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-18db0e85-99e9-c746-8531-ffeb86328b39
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-93d37703-997c-c46f-a531-755e3e0dc2ac
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-ee3e4b55-fcda-44b8-0605-64b7a9967744
...
```

Next, deploy four example apps that demonstrate how `ResourceClaim`s,
`ResourceClaimTemplate`s, and custom `ClaimParameter` objects can be used to
request access to resources in various ways:
```bash
kubectl apply --filename=gpu-test{1,2,3,4}.yaml
```

And verify that they are coming up successfully:
```console
$ kubectl get pod -A
NAMESPACE   NAME   READY   STATUS              RESTARTS   AGE
...
gpu-test1   pod0   0/1     Pending             0          2s
gpu-test1   pod1   0/1     Pending             0          2s
gpu-test2   pod0   0/2     Pending             0          2s
gpu-test3   pod0   0/1     ContainerCreating   0          2s
gpu-test3   pod1   0/1     ContainerCreating   0          2s
gpu-test4   pod0   0/1     Pending             0          2s
...
```

Use your favorite editor to look through each of the `gpu-test{1,2,3,4}.yaml`
files and see what they are doing. The semantics of each match the figure
below:

![Demo Apps Figure](demo/demo-apps.png?raw=true "Semantics of the applications requesting resources from the example DRA resource driver.")

Then dump the logs of each app to verify that GPUs were allocated to them
according to these semantics:
```bash
for example in $(seq 1 4); do \
  echo "gpu-test${example}:"
  for pod in $(kubectl get pod -n gpu-test${example} --output=jsonpath='{.items[*].metadata.name}'); do \
    for ctr in $(kubectl get pod -n gpu-test${example} ${pod} -o jsonpath='{.spec.containers[*].name}'); do \
      echo "${pod} ${ctr}:"
      kubectl logs -n gpu-test${example} ${pod} -c ${ctr}| grep GPU_DEVICE
    done
  done
  echo ""
done
```

This should produce output similar to the following:
```bash
gpu-test1:
pod0 ctr0:
declare -x GPU_DEVICE_0="GPU-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b"
pod1 ctr0:
declare -x GPU_DEVICE_0="GPU-ee3e4b55-fcda-44b8-0605-64b7a9967744"

gpu-test2:
pod0 ctr0:
declare -x GPU_DEVICE_0="GPU-9ede7e32-5825-a11b-fa3d-bab6d47e0243"
pod0 ctr1:
declare -x GPU_DEVICE_0="GPU-9ede7e32-5825-a11b-fa3d-bab6d47e0243"

gpu-test3:
pod0 ctr0:
declare -x GPU_DEVICE_0="GPU-93d37703-997c-c46f-a531-755e3e0dc2ac"
pod1 ctr0:
declare -x GPU_DEVICE_0="GPU-93d37703-997c-c46f-a531-755e3e0dc2ac"

gpu-test4:
pod0 ctr0:
declare -x GPU_DEVICE_0="GPU-18db0e85-99e9-c746-8531-ffeb86328b39"
declare -x GPU_DEVICE_1="GPU-e7b42cb1-4fd8-91b2-bc77-352a0c1f5747"
declare -x GPU_DEVICE_2="GPU-f11773a1-5bfb-e48b-3d98-1beb5baaf08e"
declare -x GPU_DEVICE_3="GPU-0159f35e-99ee-b2b5-74f1-9d18df3f22ac"
```

In this example resource driver, no "actual" GPUs are made available to any
containers. Instead, a set of environment variables are set in each container
to indicate which GPUs *would* have been injected into them by a real resource
driver.

You can use the UUIDs of the GPUs set in these environment variables to verify
that they were handed out in a way consistent with the semantics shown in the
figure above.

Likewise, looking at the `ClaimAllocations` section of the
`NodeAllocationState` object on the worker node will show which GPUs have been
allocated to a given `ResourceClaim` by the resource driver:
```console
$ kubectl describe -n dra-example-driver nas/dra-example-driver-cluster-worker
...
Spec:
  ...
  Prepared Claims:
    132ccf41-2ec6-4751-a0e5-94f3635a679a:
      Gpu:
        Devices:
          Uuid:  GPU-0159f35e-99ee-b2b5-74f1-9d18df3f22ac
    330d73e1-b5bb-40be-bc4b-2b940f1bf34f:
      Gpu:
        Devices:
          Uuid:  GPU-18db0e85-99e9-c746-8531-ffeb86328b39
    d764a8d4-4481-4bc6-959b-27695f434953:
      Gpu:
        Devices:
          Uuid:  GPU-ee3e4b55-fcda-44b8-0605-64b7a9967744
          Uuid:  GPU-e7b42cb1-4fd8-91b2-bc77-352a0c1f5747
          Uuid:  GPU-9ede7e32-5825-a11b-fa3d-bab6d47e0243
          Uuid:  GPU-f11773a1-5bfb-e48b-3d98-1beb5baaf08e
    e811664d-e487-4eb7-9ac7-678c837cbb32:
      Gpu:
        Devices:
          Uuid:  GPU-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b
    ef07764f-8dc0-4c4b-a99f-328711702e63:
      Gpu:
        Devices:
          Uuid:  GPU-93d37703-997c-c46f-a531-755e3e0dc2ac
```

Once you have verified everything is running correctly, delete all of the
example apps:
```bash
kubectl delete --wait=false --filename=gpu-test{1,2,3,4}.yaml
```

Wait for them to terminate:
```console
$ kubectl get pod -A
NAMESPACE   NAME   READY   STATUS        RESTARTS   AGE
...
gpu-test1   pod0   1/1     Terminating   0          31m
gpu-test1   pod1   1/1     Terminating   0          31m
gpu-test2   pod0   2/2     Terminating   0          31m
gpu-test3   pod0   1/1     Terminating   0          31m
gpu-test3   pod1   1/1     Terminating   0          31m
gpu-test4   pod0   1/1     Terminating   0          31m
...
```

And show that the `ClaimAllocations` section of the `NodeAllocationState`
object on the worker node is now back to its initial state:
```console
$ kubectl describe -n dra-example-driver nas/dra-example-driver-cluster-worker
...
Spec:
  Allocatable Devices:
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-18db0e85-99e9-c746-8531-ffeb86328b39
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-93d37703-997c-c46f-a531-755e3e0dc2ac
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-ee3e4b55-fcda-44b8-0605-64b7a9967744
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-9ede7e32-5825-a11b-fa3d-bab6d47e0243
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-e7b42cb1-4fd8-91b2-bc77-352a0c1f5747
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-f11773a1-5bfb-e48b-3d98-1beb5baaf08e
    Gpu:
      Product Name:  LATEST-GPU-MODEL
      Uuid:          GPU-0159f35e-99ee-b2b5-74f1-9d18df3f22ac
...
```

Finally, you can run the following to cleanup your environment and delete the
`kind` cluster started previously:
```bash
./delete-cluster.sh
```

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
