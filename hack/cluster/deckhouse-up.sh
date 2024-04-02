#!/bin/bash

function usage {
    cat <<EOF
Usage: $(basename "$0") COMMAND OPTIONS

Commands:
  deploy          Create deckhouse cluster in DVP. Include prepare-infra and bootstrap.
  ---
  prepare-infra   Create infra only.
                  Arguments:
                  (Optional) --storage-class: storage-class for virtual machine disks. If not defined - using default SC.
                  It is possible to use the environment variable: DVP_STORAGE_CLASS.
  ---
  bootstrap       Bootstrap tenant cluster with dhctl.
                  Arguments:
                  (Required) --registry-docker-cfg: key to access the Docker registry in base64 format.
                  It is possible to use the environment variable: TENANT_REGISTRY_DOCKER_CFG.
                  (Optional) --kubernetes-version (default Automatic): version of kubernetes that will be installed.
                  It is possible to use the environment variable: TENANT_KUBERNETES_VERSION.
                  (Optional) --only-control-plane (default false): Bootstrap only control-plane.
                  (Optional) --only-compute-nodes (default false): Bootstrap only worker nodes.
  ---
  kubeconfig      Kubeconfig save in local config.
                  Arguments:
                  (Optional) --save-dir (default /tmp/deckhouse-tenant-cluster): path to the directory where the config will be saved.
  ---
  destroy         Destroy deckhouse cluster.

Global Arguments:
  --name (default tenant-cluster): name for tenant cluster.
    It is possible to use the environment variable: TENANT_CLUSTER_NAME
  --namespace (default tenant-cluster): namespace for tenant cluster.
    It is possible to use the environment variable: DVP_NAMESPACE.

Examples:
  Deploy:
    export TENANT_REGISTRY_DOCKER_CFG="WW91cktleQo="
    $(basename "$0") deploy --namespace=mynamespace --storage-class=mysc
  Bootstrap:
    $(basename "$0") bootstrap --namespace=mynamespace --registry-docker-cfg=WW91cktleQo=
  Destroy:
    $(basename "$0") destroy --namespace=mynamespace
EOF
  exit 1
}

function handle_exit() {
  for p in $(jobs -p); do pgrep -P "${p}" | xargs kill -9 ; done
}

function validate_deploy_args() {
  validate_prepare_infra_args
  validate_bootstrap_args
}

function validate_prepare_infra_args() {
  if [ -z "${STORAGE_CLASS}" ] ; then
    die_if_default_storageClass_does_not_exist
  fi
}

function validate_bootstrap_args() {
  if [ -z "${REGISTRY_DOCKER_CFG}" ]; then
    echo "ERROR: REGISTRY_DOCKER_CFG is not defined"
    usage
  fi
}

function wait_vm() {
  local NODE=$1
  d8_kubectl -n "${NAMESPACE}" wait vm "${NODE}" --for='jsonpath={.status.phase}=Running' --timeout=1200s
}

function deploy() {
  echo "Deploy tenant cluster in namespace ${NAMESPACE}"
  prepare_infra
  bootstrap
}
function prepare_infra() {
  echo "Prepare infra for tenant cluster in namespace ${NAMESPACE}"
  helm upgrade --install "${CLUSTER_NAME}" "${SCRIPT_DIR}/tenant-cluster" -n "${NAMESPACE}" --set "storageClass=${STORAGE_CLASS}" --create-namespace
  for node in $(d8_kubectl get vm -o jsonpath='{.items[*].metadata.name}' -l "app.kubernetes.io/instance=${CLUSTER_NAME}" -n "${NAMESPACE}") ; do
    wait_vm "${node}"
  done
}

function bootstrap() {
  echo "Bootstrap deckhouse in ${NAMESPACE}"
  if [ "$ONLY_CONTROL_PLANE" == "true" ]; then
    bootstrap_control_plane
    return
  fi
  if [ "$ONLY_COMPUTE_NODES" == "true" ]; then
    bootstrap_nodes
    return
  fi
  bootstrap_control_plane
  sleep 60
  bootstrap_nodes
}

function bootstrap_control_plane() {
  echo "Bootstrap control-plane in ${NAMESPACE}"
  local PORT=$(get_free_port)
  port_forward_start "${PORT}" "22"

  docker run --rm -it --network host -v "${SCRIPT_DIR}/ssh:/ssh:ro" -v "${SCRIPT_DIR}/config:/config:ro" \
    -e INTERNAL_NETWORK_CIDR=$(discovery_vm_cidr) -e REGISTRY_DOCKER_CFG="${REGISTRY_DOCKER_CFG}"        \
    -e KUBERNETES_VERSION="${KUBERNETES_VERSION}" "${DEV_REGISTRY}/${INSTALL_IMAGE}:${INSTALL_TAG}"      \
     /bin/bash -c "envsubst < /config/config.yaml > /tmp/config.yaml && dhctl bootstrap --ssh-user=${USERNAME} --ssh-host=127.0.0.1 --ssh-port=${PORT} --ssh-agent-private-keys=/ssh/id_ed --config=/tmp/config.yaml"

}

function bootstrap_nodes() {
  echo "Bootstrap nodes. Join in deckhouse cluster."
  local PORT=$(get_free_port)
  get_kubeconfig "${PORT}" "${SAVE_KUBECONFIG_DIR}"
  local CONFIG="${SAVE_KUBECONFIG_DIR}/kubeconfig"
  port_forward_start "${PORT}" "6443"
  d8_kubectl --kubeconfig "${CONFIG}" wait --for condition=Ready -n d8-system pod -l app=deckhouse
  d8_kubectl --kubeconfig "${CONFIG}" apply -f "${SCRIPT_DIR}/config/nodegroup-worker.yaml"
  d8_kubectl --kubeconfig "${CONFIG}" wait ng worker --for='jsonpath={.status.deckhouse.synced}=True' --timeout=1200s
  while ! d8_kubectl --kubeconfig "${CONFIG}" get secret manual-bootstrap-for-worker -n d8-cloud-instance-manager &>/dev/null;
    do echo "Waiting for d8-cloud-instance-manager/manual-bootstrap-for-worker. CTRL-C to exit."
    sleep 1
  done
  CMD_JOIN=$(d8_kubectl --kubeconfig "${CONFIG}" -n d8-cloud-instance-manager get secret manual-bootstrap-for-worker -o jsonpath='{.data.bootstrap\.sh}')
  for worker in $(d8_kubectl get vm -o jsonpath='{.items[*].metadata.name}' -l "app.kubernetes.io/instance=${CLUSTER_NAME},role=worker" -n "${NAMESPACE}"); do
    ssh_exec_command "${worker}" "echo $CMD_JOIN | base64 -d | sudo bash"
  done
}

function ssh_exec_command() {
  NODE=$1
  COMMAND=$2
  d8_virtualization ssh --local-ssh --local-ssh-opts='-o StrictHostKeyChecking=no'            \
    --local-ssh-opts='-o UserKnownHostsFile=/dev/null' --local-ssh-opts='-o LogLevel=ERROR'   \
    -i "${SCRIPT_DIR}/ssh/id_ed" --username="${USERNAME}" --namespace="${NAMESPACE}" "${NODE}" --command "${COMMAND}"
}

function get_kubeconfig() {
  local PORT=$1
  local DIR=$2
  local CONFIG="${DIR}/kubeconfig"
  echo "Save kubeconfig to ${CONFIG}"
  if ! [ -d "${DIR}" ]; then
    mkdir -p "${DIR}"
  fi
  ssh_exec_command "${MASTER_NAME}" "sudo cat /etc/kubernetes/admin.conf" | sed 's!server:.*!server: https://127.0.0.1:'"${PORT}"'!g' > "${CONFIG}"
}

function kubeconfig_usage() {
  local PORT=$1
  cat <<EOF
1. Start port-forward to master node
d8 virtualization port-forward ${MASTER_NAME}.${NAMESPACE} tcp/${PORT}:6443
2. Check connect to cluster
kubectl --kubeconfig ${SAVE_KUBECONFIG_DIR}/kubeconfig get node
EOF
}

function port_forward_start() {
  # TODO: sometimes the session ends and the cluster fails to complete booting after reboot
  # TODO: fast fix, ps aux | grep forward. copy the command and run
  local LOCAL_PORT=$1
  local TARGET_PORT=$2
  echo "Starting port-forward for ${MASTER_NAME}.${NAMESPACE}:${PORT}"
  (d8_virtualization port-forward "${MASTER_NAME}.${NAMESPACE}" "tcp/${LOCAL_PORT}:${TARGET_PORT}" &>/dev/null) &
}

function destroy() {
  echo "Destroy tenant cluster in namespace ${NAMESPACE}"
  helm uninstall "${CLUSTER_NAME}" -n "${NAMESPACE}"
}

function die_if_default_storageClass_does_not_exist() {
  out=$(d8_kubectl get sc -o jsonpath='{.items[*].metadata.annotations.storageclass\.kubernetes\.io/is-default-class}')
  if ! [[ "${out}" =~ "true" ]]; then
    echo "ERROR: Default StorageClass not found"
    echo "Use flag --storage-class or see https://kubernetes.io/docs/tasks/administer-cluster/change-default-storage-class/"
    exit 1
  fi
}

function discovery_vm_cidr() {
  IP_ADDRESS=$(kubectl get vm "${MASTER_NAME}" -o jsonpath='{.status.ipAddress}')
  for cidr in $(kubectl get mc virtualization -o jsonpath='{.spec.settings.virtualMachineCIDRs[*]}'); do
    if [ $(is_ip_in_cidr "${IP_ADDRESS}" "${cidr}") == "true" ]; then
      echo "${cidr}"
      return
    fi
  done
}

function set_master() {
  MASTER_NAME=$(kubectl get vm -n "${NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' -l "app.kubernetes.io/instance=${CLUSTER_NAME},role=master")
}

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd -P)"
chmod 700 "${SCRIPT_DIR}/ssh"
chmod 600 "${SCRIPT_DIR}/ssh/id_ed"
source "${SCRIPT_DIR}/common.sh"

if [ "$#" -eq 0 ] || [ "${1}" == "--help" ] ; then
  usage
  exit
fi

trap 'handle_exit' EXIT INT ERR

COMMAND=$1
shift
# Set naming variable
while [[ $# -gt 0 ]]; do
    case "$1" in
    --name=*)
        CLUSTER_NAME="${1#*=}"
        shift
        ;;
    --namespace=*)
        NAMESPACE="${1#*=}"
        shift
        ;;
    --storage-class=*)
        STORAGE_CLASS="${1#*=}"
        shift
        ;;
    --registry-docker-cfg=*)
        REGISTRY_DOCKER_CFG="${1#*=}"
        shift
        ;;
    --kubernetes-version=*)
        KUBERNETES_VERSION="${1#*=}"
        shift
        ;;
    --only-control-plane=*)
        ONLY_CONTROL_PLANE="${1#*=}"
        shift
        ;;
    --only-compute-nodes=*)
        ONLY_COMPUTE_NODES="${1#*=}"
        shift
        ;;
    --save-dir=*)
        SAVE_KUBECONFIG_DIR="${1#*=}"
        shift
        ;;
    *)
        echo "ERROR: Invalid argument: $1"
        usage
        ;;
    esac
done

case "${COMMAND}" in
  deploy)
    validate_deploy_args
    deploy
    ;;
  prepare-infra)
    validate_prepare_infra_args
    prepare_infra
    ;;
  bootstrap)
    set_master
    validate_bootstrap_args
    bootstrap
    ;;
  destroy)
    destroy
    ;;
  kubeconfig)
    set_master
    port=$(get_free_port)
    get_kubeconfig "${port}" "${SAVE_KUBECONFIG_DIR}"
    kubeconfig_usage "${port}"
    ;;
*)
    echo "ERROR: Invalid argument: ${COMMAND}"
    usage
    ;;
esac
