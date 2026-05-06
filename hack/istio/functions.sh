#!/bin/bash
# shellcheck disable=SC2155

# This file contains useful functions which are used in other hack scripts in this dirrectory.

# ensure_gateway_api_crds [version] [context_args]
# Installs Gateway API CRDs if not already present.
# Uses K8S_GATEWAY_API_VERSION if set, else defaults to v1.5.0.
# Uses CLIENT_EXE (kubectl/oc) for cluster operations.
# Optional: version - override (e.g. v1.5.0); context_args - for multicluster (e.g. "--context=kind-dataplane")
ensure_gateway_api_crds() {
  local version="${1:-${K8S_GATEWAY_API_VERSION:-v1.5.0}}"
  local context_args="${2:-}"
  local client="${CLIENT_EXE:-kubectl}"

  if [ -z "${version}" ]; then
    version=$(curl --head --silent "https://github.com/kubernetes-sigs/gateway-api/releases/latest" 2>/dev/null | grep "location: " | awk '{print $2}' | sed "s/.*tag\///g" | tr -d '\r' || echo "v1.5.0")
  fi

  echo "Verifying that Gateway API is installed; if it is not then Gateway API version ${version} will be installed now."
  ${client} get crd gateways.gateway.networking.k8s.io ${context_args} &> /dev/null || \
    { ${client} kustomize "github.com/kubernetes-sigs/gateway-api/config/crd?ref=${version}" | ${client} apply -f - ${context_args}; }
}

# Wait for istiod to be reachable before applying resources that may trigger
# the validating webhook. Recent Istio versions can report the install as ready
# slightly before the webhook backend is actually serving.
wait_for_istiod_ready() {
  local namespace="${1:-istio-system}"
  local timeout="${2:-300s}"
  local client="${CLIENT_EXE:-kubectl}"
  local sleep_seconds=5
  local timeout_seconds="${timeout%s}"
  local max_attempts
  local attempt=0

  if ! [[ "${timeout_seconds}" =~ ^[0-9]+$ ]]; then
    timeout_seconds=300
  fi
  max_attempts=$(((timeout_seconds + sleep_seconds - 1) / sleep_seconds))

  echo "Waiting for istiod deployment in namespace [${namespace}]"
  while [ "${attempt}" -lt "${max_attempts}" ]; do
    if ${client} wait --for=condition=Available deployment -l app=istiod -n "${namespace}" --timeout="${sleep_seconds}s" >/dev/null 2>&1; then
      return 0
    fi

    echo "istiod is not ready yet; retrying in ${sleep_seconds}s"
    attempt=$((attempt + 1))
  done

  echo "ERROR: Timed out waiting for istiod to be ready in namespace [${namespace}]"
  ${client} get deployments -n "${namespace}" -l app=istiod || true
  return 1
}

# Returns 0 if a smcp in given namespaces contains .spec.mode=ClusterWide, 1 otherwise.
is_cluster_wide() {
  local mode=$(${CLIENT_EXE} get smcp -n ${ISTIO_NAMESPACE} -o=jsonpath='{.items[0].spec.mode}' 2> /dev/null || true)
  if [ "${mode}" = "ClusterWide" ]
    then
      # 0 = true
      return 0
    else
      # 1 = false
      return 1
  fi
}

# Returns 0 if the istio version is greater than specified, 0 otherwise.
is_istio_version_eq_greater_than() {
  local expected_version=$1
  local istio_version=$(${ISTIOCTL} version)
  istio_parsed_version=$(echo "$istio_version" | grep "client version" | awk '{print $3}' | cut -d'-' -f1)

  istio_expected_version=$(echo "$expected_version" | cut -d'-' -f1)

  IFS='.' read -r major minor _patch <<< "$istio_parsed_version"
  IFS='.' read -r major_expected minor_expected _patch_expected <<< "$istio_expected_version"
  IFS=' '
  if [ "${major}" -lt "${major_expected}" ]; then
    return 1
  else
    if [ "${major}" -eq "${major_expected}" ]; then
      if [ "${minor}" -lt "${minor_expected}" ]; then
        return 0
      else
        return 1
      fi
    else
      return 0
    fi
  fi

}
