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
NAME                                             READY   STATUS    RESTARTS   AGE
dra-example-driver-kubeletplugin-qwmbl           1/1     Running   0          1m
```

And show the initial state of available GPU devices on the worker node:
```
$ kubectl get resourceslice -o yaml
apiVersion: v1
items:
- apiVersion: resource.k8s.io/v1alpha3
  kind: ResourceSlice
  metadata:
    creationTimestamp: "2024-07-16T13:00:06Z"
    generateName: dra-example-driver-cluster-worker-gpu.example.com-
    generation: 1
    name: dra-example-driver-cluster-worker-gpu.example.com-xhsqf
    ownerReferences:
    - apiVersion: v1
      controller: true
      kind: Node
      name: dra-example-driver-cluster-worker
      uid: 8a18f216-bd77-426a-86b1-a989bdacc135
    resourceVersion: "854"
    uid: ea955e01-eaf0-45ed-8a36-1c56e4b8bced
  spec:
    driver: gpu.example.com
    nodeName: dra-example-driver-cluster-worker
    pool:
      generation: 0
      name: dra-example-driver-cluster-worker
      resourceSliceCount: 1
    devices:
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 0
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-18db0e85-99e9-c746-8531-ffeb86328b39
        capacity:
          memory: 80Gi
      name: gpu-18db0e85-99e9-c746-8531-ffeb86328b39
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 1
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-93d37703-997c-c46f-a531-755e3e0dc2ac
        capacity:
          memory: 80Gi
      name: gpu-93d37703-997c-c46f-a531-755e3e0dc2ac
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 2
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-ee3e4b55-fcda-44b8-0605-64b7a9967744
        capacity:
          memory: 80Gi
      name: gpu-ee3e4b55-fcda-44b8-0605-64b7a9967744
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 3
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-9ede7e32-5825-a11b-fa3d-bab6d47e0243
        capacity:
          memory: 80Gi
      name: gpu-9ede7e32-5825-a11b-fa3d-bab6d47e0243
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 4
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-e7b42cb1-4fd8-91b2-bc77-352a0c1f5747
        capacity:
          memory: 80Gi
      name: gpu-e7b42cb1-4fd8-91b2-bc77-352a0c1f5747
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 5
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-f11773a1-5bfb-e48b-3d98-1beb5baaf08e
        capacity:
          memory: 80Gi
      name: gpu-f11773a1-5bfb-e48b-3d98-1beb5baaf08e
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 6
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-0159f35e-99ee-b2b5-74f1-9d18df3f22ac
        capacity:
          memory: 80Gi
      name: gpu-0159f35e-99ee-b2b5-74f1-9d18df3f22ac
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 7
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b
        capacity:
          memory: 80Gi
      name: gpu-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b
kind: List
metadata:
  resourceVersion: ""
```

Next, deploy four example apps that demonstrate how `ResourceClaim`s,
`ResourceClaimTemplate`s, and custom `ClaimParameter` objects can be used to
request access to resources in various ways:
```bash
kubectl apply --filename=demo/gpu-test{1,2,3,4}.yaml
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
declare -x GPU_DEVICE_0="gpu-e7b42cb1-4fd8-91b2-bc77-352a0c1f5747"
pod1 ctr0:
declare -x GPU_DEVICE_0="gpu-f11773a1-5bfb-e48b-3d98-1beb5baaf08e"

gpu-test2:
pod0 ctr0:
declare -x GPU_DEVICE_0="gpu-0159f35e-99ee-b2b5-74f1-9d18df3f22ac"
pod0 ctr1:
declare -x GPU_DEVICE_0="gpu-0159f35e-99ee-b2b5-74f1-9d18df3f22ac"

gpu-test3:
pod0 ctr0:
declare -x GPU_DEVICE_0="gpu-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b"
pod1 ctr0:
declare -x GPU_DEVICE_0="gpu-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b"

gpu-test4:
pod0 ctr0:
declare -x GPU_DEVICE_0="gpu-18db0e85-99e9-c746-8531-ffeb86328b39"
declare -x GPU_DEVICE_1="gpu-93d37703-997c-c46f-a531-755e3e0dc2ac"
declare -x GPU_DEVICE_2="gpu-ee3e4b55-fcda-44b8-0605-64b7a9967744"
declare -x GPU_DEVICE_3="gpu-9ede7e32-5825-a11b-fa3d-bab6d47e0243"
```

In this example resource driver, no "actual" GPUs are made available to any
containers. Instead, a set of environment variables are set in each container
to indicate which GPUs *would* have been injected into them by a real resource
driver.

You can use the UUIDs of the GPUs set in these environment variables to verify
that they were handed out in a way consistent with the semantics shown in the
figure above.

Once you have verified everything is running correctly, delete all of the
example apps:
```bash
kubectl delete --wait=false --filename=demo/gpu-test{1,2,3,4}.yaml
```

And wait for them to terminate:
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

Finally, you can run the following to cleanup your environment and delete the
`kind` cluster started previously:
```bash
./demo/delete-cluster.sh
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
