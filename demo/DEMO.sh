# Build the example resource driver
./build-driver.sh

# Create a test cluster
./create-cluster.sh

# Show current state of the cluster
kubectl get pod -A

# Install the example resource driver
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  dra-example-driver \
  ../deployments/helm/dra-example-driver

# Show the initial node allocation state
kubectl describe -n dra-example-driver nas/dra-example-driver-cluster-worker

# Deploy the 4 example apps discussed in the slides
kubectl apply --filename=gpu-test{1,2,3,4}.yaml

# Show all the pods starting up
kubectl get pod -A

# Show the yaml files for the first 3 example apps
vim -O gpu-test1.yaml gpu-test2.yaml gpu-test3.yaml

# Show the yaml file for the last example app
vim -O gpu-test4.yaml

# Show the pods running
kubectl get pod -A

# Show the GPUs allocated to each
for example in $(seq 1 4); do \
  echo "gpu-test${example}:"
  for pod in $(kubectl get pod -n gpu-test${example} --output=jsonpath='{.items[*].metadata.name}'); do \
    for ctr in $(kubectl get pod -n gpu-test${example} ${pod} -o jsonpath='{.spec.containers[*].name}'); do \
      echo "${pod} ${ctr}:"
      kubectl logs -n gpu-test${example} ${pod} -c ${ctr}| grep -i gpu
    done
  done
  echo ""
done

# Show the current node allocation state
kubectl describe -n dra-example-driver nas/dra-example-driver-cluster-worker

# Delete all examples
kubectl delete --wait=false --filename=gpu-test{1,2,3,4}.yaml

# Show the pods terminating
kubectl get pod -A

# Show the final node allocation state
kubectl describe -n dra-example-driver nas/dra-example-driver-cluster-worker
