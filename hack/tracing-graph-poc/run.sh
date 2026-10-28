#!/bin/bash

##############################################################################
# run.sh
#
# Entry point for the Bookinfo trace-topology PoC (kiali/kiali#10413).
#
# Usage:
#   hack/tracing-graph-poc/run.sh up       # full setup (cluster + mesh + bookinfo)
#   hack/tracing-graph-poc/run.sh access   # port-forwards
#   hack/tracing-graph-poc/run.sh verify   # checks (needs access)
##############################################################################

set -euo pipefail

# shellcheck source=hack/tracing-graph-poc/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

ensure_kind_cluster() {
  if kind get clusters 2>/dev/null | grep -qx "${POC_CLUSTER}"; then
    infomsg "Kind cluster [${POC_CLUSTER}] already exists"
    ${CLIENT_EXE} config use-context "kind-${POC_CLUSTER}" >/dev/null
    return 0
  fi

  local kind_args=(-n "${POC_CLUSTER}")
  if [ -n "${POC_KIND_IMAGE:-}" ]; then
    kind_args+=(-i "${POC_KIND_IMAGE}")
  fi
  infomsg "Creating Kind cluster [${POC_CLUSTER}] via hack/start-kind.sh"
  "${REPO_DIR}/hack/start-kind.sh" "${kind_args[@]}"
  ${CLIENT_EXE} config use-context "kind-${POC_CLUSTER}" >/dev/null
}

case "${1:-up}" in
  up)
    ensure_kind_cluster
    "${POC_DIR}/observability.sh"
    "${POC_DIR}/bookinfo.sh"
    infomsg "PoC ready. Run: ${POC_DIR}/run.sh access  (then verify in another terminal)"
    ;;
  bookinfo)
    # Re-run only the app layer (mesh/observability already installed).
    "${POC_DIR}/bookinfo.sh"
    ;;
  access)
    pids=()
    trap 'kill "${pids[@]}" 2>/dev/null || true' EXIT
    ${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" port-forward svc/kiali 21001:20001 &
    pids+=("$!")
    ${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" port-forward svc/prometheus 19090:9090 &
    pids+=("$!")
    ${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" port-forward svc/tracing 17686:80 &
    pids+=("$!")
    if ${CLIENT_EXE} -n bookinfo get service productpage >/dev/null 2>&1; then
      ${CLIENT_EXE} -n bookinfo port-forward svc/productpage 18080:9080 &
      pids+=("$!")
      echo "Bookinfo: http://localhost:18080/productpage"
    fi
    echo "Kiali: http://localhost:21001/kiali | Prometheus: http://localhost:19090 | Jaeger: http://localhost:17686"
    wait -n
    ;;
  verify|verify-bookinfo)
    "${POC_DIR}/verify.sh"
    ;;
  status)
    ${CLIENT_EXE} get pods -n "${ISTIO_NAMESPACE}"
    ${CLIENT_EXE} get pods -n bookinfo 2>/dev/null || true
    ;;
  down)
    kind delete cluster --name "${POC_CLUSTER}"
    ;;
  *)
    errormsg "Usage: $0 {up|bookinfo|access|verify|verify-bookinfo|status|down}"
    exit 2
    ;;
esac
