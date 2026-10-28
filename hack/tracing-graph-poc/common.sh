#!/bin/bash

##############################################################################
# common.sh
#
# Shared settings for the tracing-graph PoC under hack/tracing-graph-poc.
##############################################################################

# shellcheck disable=SC2034
POC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${POC_DIR}/../.." && pwd)"
POC_CLUSTER="${POC_CLUSTER:-tracing-graph-poc}"
ISTIO_NAMESPACE="${ISTIO_NAMESPACE:-istio-system}"
CLIENT_EXE="${CLIENT_EXE:-kubectl}"
POC_OUT="${POC_OUT:-${REPO_DIR}/_output/tracing-graph-poc}"

# Mesh cluster name used by Sail by default and by extension recording-rule labels.
POC_MESH_CLUSTER="${POC_MESH_CLUSTER:-cluster-default}"

export POC_DIR REPO_DIR POC_CLUSTER ISTIO_NAMESPACE CLIENT_EXE POC_OUT POC_MESH_CLUSTER

mkdir -p "${POC_OUT}"

infomsg() {
  echo "[INFO] ${1}"
}

errormsg() {
  echo "[ERROR] ${1}" >&2
}
