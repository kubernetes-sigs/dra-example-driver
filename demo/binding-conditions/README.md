### Demo DRA Device Binding Conditions Feature
This example driver includes support for the [DRA Device Binding Conditions feature](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/#device-binding-conditions), which allows drivers to declare conditions that must be met before a device is considered fully bound and ready for use.
When enabled, each GPU device in the `ResourceSlice` is published with a `bindingConditions` list and a `bindingFailureConditions` list.
The scheduler uses these to track the binding lifecycle of allocated devices.

#### Usage Example

1. **Enable binding conditions**: Binding conditions require two settings: `kubeletPlugin.bindingConditions` to publish binding conditions in the `ResourceSlice`, and `controller.plugins={BindingConditions}` to deploy the controller that automatically satisfies them. Set both in `values.yaml` or via `--set`:
    ```bash
    helm upgrade -i \
      --create-namespace \
      --namespace dra-example-driver \
      --set kubeletPlugin.bindingConditions=true \
      --set controller.plugins={BindingConditions} \
      dra-example-driver \
      deployments/helm/dra-example-driver
    ```

    **Note**: The `DRADeviceBindingConditions` feature gate must also be enabled on the Kubernetes cluster.
    The demo Kind cluster configuration (`demo/scripts/kind-cluster-config.yaml`) already includes this gate.

2. **Verify the ResourceSlice**: After installing the driver with binding conditions enabled, inspect the `ResourceSlice` to confirm that devices are published with binding condition fields:
    ```bash
    kubectl get resourceslice -o yaml
    ```

    Each device should contain:
    ```yaml
    bindingConditions:
    - BindingConditions
    bindingFailureConditions:
    - BindingFailureConditions
    ```

3. **Create the demo pod**: Apply the example manifest. The `BindingConditions` controller plugin automatically watches allocated `ResourceClaim` objects and satisfies binding conditions, so the pod transitions from `Pending` to `Running` shortly after creation:
    ```bash
    kubectl apply -f demo/binding-conditions/binding-conditions.yaml
    ```

    ```console
    $ kubectl get pod -n binding-conditions
    NAME   READY   STATUS    RESTARTS   AGE
    pod0   1/1     Running   0          30s
    ```

    Verify that the `ResourceClaim` has been allocated and that `status.devices` contains the `BindingConditions` condition set to `True`:
    ```console
    $ kubectl get resourceclaim -n binding-conditions
    NAME             STATE                AGE
    pod0-gpu-5bnfq   allocated,reserved   30s
    ```

    ```bash
    kubectl get resourceclaim -n binding-conditions -o yaml
    ```

    ```yaml
    status:
      allocation:
        devices:
          results:
          - bindingConditions:
            - BindingConditions
            bindingFailureConditions:
            - BindingFailureConditions
            device: gpu-0
            driver: gpu.example.com
            pool: dra-example-driver-cluster-worker
            request: gpu
      devices:
      - conditions:
        - lastTransitionTime: "2026-05-15T14:00:00Z"
          message: Device is ready
          reason: Ready
          status: "True"
          type: BindingConditions
        device: gpu-0
        driver: gpu.example.com
        pool: dra-example-driver-cluster-worker
      reservedFor:
      - name: pod0
        resource: pods
    ```

#### Testing

E2e tests cover binding conditions validation. The test suite installs the
driver with the required Helm values automatically, so no special environment
variables are needed:

```bash
# Set up the cluster
make setup-e2e

# Run the e2e tests (includes binding conditions tests)
make test-e2e

# Run only the binding conditions tests
go run github.com/onsi/ginkgo/v2/ginkgo --tags=e2e --focus="BindingConditions" ./test/e2e/...

# Tear down the cluster
make teardown-e2e
```
