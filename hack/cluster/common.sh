#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd -P)"
USERNAME="cloud"
CLUSTER_NAME="${TENANT_CLUSTER_NAME:-tenant-cluster}"
NAMESPACE="${DVP_NAMESPACE:-tenant-cluster}"
KUBERNETES_VERSION="${TENANT_KUBERNETES_VERSION:-Automatic}"
STORAGE_CLASS="${DVP_STORAGE_CLASS}"
REGISTRY_DOCKER_CFG="${TENANT_REGISTRY_DOCKER_CFG}"
ONLY_CONTROL_PLANE=false
ONLY_COMPUTE_NODES=false
DEV_REGISTRY="dev-registry.deckhouse.io"
INSTALL_IMAGE="sys/deckhouse-oss/install"
INSTALL_TAG="main"

SAVE_KUBECONFIG_DIR="/tmp/deckhouse-tenant-cluster"

which d8 &>/dev/null || (echo "Command 'd8' not found" ; exit 1)
which helm &>/dev/null || (echo "Command 'helm' not found" ; exit 1)
which docker &>/dev/null || (echo "Command 'docker' not found" ; exit 1)

function d8_kubectl() {
    d8 kubectl "$@"
}

function d8_virtualization() {
  d8 virtualization "$@"
}

function random_port() {
  echo $((10000 + $RANDOM % 20000))
}

function get_free_port() {
  while [[ ${res} -eq 0 ]]; do
    port=$(random_port)
    ss -tl | grep $port
    res=$?
  done
  echo $port
}

function is_ip_in_cidr {
  local ip=$1
  local cidr=$2
  local network=$(echo "${cidr}" | cut -d/ -f1)
  local prefix=$(echo "${cidr}" | cut -d/ -f2)
  local ip_arr=($(echo "${ip}" | tr '.' ' '))
  local network_arr=($(echo "${network}" | tr '.' ' '))
  local network_dec=$(( (network_arr[0] << 24) + (network_arr[1] << 16) + (network_arr[2] << 8) + network_arr[3] ))
  local ip_dec=$(( (ip_arr[0] << 24) + (ip_arr[1] << 16) + (ip_arr[2] << 8) + ip_arr[3] ))
  local mask_dec=$(( 0xffffffff << (32 - "${prefix}") ))
  if [[ $((ip_dec & mask_dec)) -eq $((network_dec & mask_dec)) ]]; then
    echo "true"
  else
    echo "false"
  fi
}