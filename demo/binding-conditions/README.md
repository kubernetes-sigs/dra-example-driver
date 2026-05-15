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
    bindsToNode: true
    bindingConditions:
    - BindingConditions
    bindingFailureConditions:
    - BindingFailureConditions
    ```

3. **Create the demo pod**: Apply the example manifest and confirm that the pod stays `Pending` because the binding conditions have not yet been satisfied:
    ```bash
    kubectl apply -f demo/binding-conditions/binding-conditions.yaml
    ```

    ```console
    $ kubectl get pod -n binding-conditions
    NAME   READY   STATUS    RESTARTS   AGE
    pod0   0/1     Pending   0          14s
    ```
    Also verify that the `ResourceClaim` has been allocated and that `bindingConditions` and `bindingFailureConditions` appear in its status:
    ```console
    $ kubectl get resourceclaim -n binding-conditions
    NAME             STATE                AGE
    pod0-gpu-5bnfq   allocated,reserved   5m14s
    ```

    ```console
    $ kubectl get resourceclaim -n binding-conditions -o yaml
    ...
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
      reservedFor:
      - name: pod0
        resource: pods
    ...
    ```

4. **Binding condition is satisfied automatically**: The `BindingConditions` controller plugin watches allocated `ResourceClaim` objects and automatically marks binding conditions as satisfied. After a short reconciliation period, the pod should transition out of `Pending`:
    ```console
    $ kubectl get pod -n binding-conditions
    NAME   READY   STATUS    RESTARTS   AGE
    pod0   1/1     Running   0          2m
    ```

    Verify that `status.devices` on the `ResourceClaim` now contains the `BindingConditions` condition set to `True`:
    ```bash
    kubectl get resourceclaim -n binding-conditions -o yaml
    ```

    ```yaml
    status:
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
    ```

#### Testing

E2e tests cover binding conditions validation. To run them:

```bash
# Set up the cluster with binding conditions enabled
BINDING_CONDITIONS=true make setup-e2e

# Run the e2e tests (includes binding conditions tests)
BINDING_CONDITIONS=true make test-e2e

# Run only the binding conditions tests
BINDING_CONDITIONS=true go run github.com/onsi/ginkgo/v2/ginkgo --tags=e2e --focus="BindingConditions" ./test/e2e/...

# Tear down the cluster
make teardown-e2e
```
