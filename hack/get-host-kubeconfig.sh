#!/usr/bin/env bash

parse_parameters(){
  shift
  # Set naming variable
  while [[ $# -gt 0 ]]; do
      case "$1" in
      --server=*)
        SERVER="${1#*=}"
        shift
        ;;
      --namespace=*)
        NAMESPACE="${1#*=}"
        shift
        ;;
      *)
        echo "ERROR: Invalid argument: $1"
        usage
        ;;
      esac
  done

  if [[ -z $SERVER ]]; then
    echo "Server parameter missed but required"
    exit 1
  fi
  if [[ -z $NAMESPACE ]]; then
    echo "Server parameter missed but required"
    exit 1
  fi
}

echo_kubeconfig_base64(){
  CERT=$(kubectl get secret -n "${NAMESPACE}" dvp-cloud-controller-manager-secret -o jsonpath='{.data.ca\.crt}')
  TOKEN=$(kubectl get secret -n "${NAMESPACE}"  dvp-cloud-controller-manager-secret -o jsonpath='{.data.token}' | base64 --decode)

  config="""apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: $CERT
    server: $SERVER
  name: ccm
contexts:
- context:
    cluster: ccm
    namespace: $NAMESPACE
    user: ccm
  name: ccm@ccm
current-context: ccm@ccm
kind: Config
preferences: {}
users:
- name: ccm
  user:
    token: $TOKEN"""
  echo "$config" | base64 -w 0
  echo
}

main(){
  parse_parameters "$@"
  echo_kubeconfig_base64
}

main "$@"