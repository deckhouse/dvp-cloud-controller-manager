# DVP Cloud Controller Manager

## Overview
`dvp-cloud-controller-manager` is the Kubernetes Cloud Controller Manager (CCM) implementation for DVP (Deckhouse Virtualization Platform).
Cloud on your Kubernetes clusters.
Read more about Kubernetes CCM [here](https://kubernetes.io/docs/tasks/administer-cluster/running-cloud-controller/).

## Work In Progress
This project is currently under active development. Use at your own risk!
Contributions welcome!

## Getting Started

### In DVP (host cluster)
1. Modify `deploy/host/kustomization.yaml` by setting the _namespace_ according to the virtual machines
   on which the guest cluster is deployed:
```deploy/host/kustomization.yaml
namespace: default
resources:
- rbac.yaml
```

2. Create service account with roles required for `dvp-cloud-controller-manager`:
```shell
kubectl apply -k deploy/host/
```
3. Create base64 of host cluster kubeconfig for the `dvp-cloud-controller-manager` service account::
```shell
# ! Change SERVER and NAMESPACE values below ! 
SERVER="https://your-server:6443"
NAMESPACE="your namespace"
curl -s https://github.com/deckhouse/virtualization-cloud-controller-manager/main/hack/get-host-kubeconfig.sh | bash /dev/stdin --server=$SERVER --namespace=$NAMESPACE
```
### In guest cluster:

1. Create `user-values.yaml` with the following parameters:
```user-values.yaml
## Required
clusterName: 
   # tenant cluster name
podCIDR:
   # tenant cluster podCIDR
kubeconfigDataBase64: 
   # generated kubeconfig in base64 format
## Optional
config: {...}
   # config for ccm. example - deploy/guest/config.yaml
```

2. Install `dvp-cloud-controller-manager` to guest cluster:
```shell
helm upgrade --install ccm deploy/guest/dvp-cloud-controller-manager -f user-values.yaml
```
