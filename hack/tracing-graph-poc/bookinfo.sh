#!/bin/bash

##############################################################################
# bookinfo.sh
#
# Deploys Bookinfo via the shared Kiali installer, applies traces-only
# Telemetry (Istio traffic metrics disabled), and injects a sidecar into the
# shared traffic generator so the first CLIENT edge is observable.
##############################################################################

set -euo pipefail

# shellcheck source=hack/tracing-graph-poc/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

${CLIENT_EXE} get namespace "${ISTIO_NAMESPACE}" >/dev/null

# Avoid install-bookinfo-demo's OpenShift heuristic: CLIENT_EXE built as
# "kubectl --context=...-poc" matches *"oc" because the context name ends in "poc".
# Use the current context instead (set by run.sh up / start-kind).
${CLIENT_EXE} config use-context "kind-${POC_CLUSTER}" >/dev/null

# Download Bookinfo manifests (latest unless ISTIO_VERSION is set).
download_args=(--output "${POC_OUT}/istio")
if [ -n "${ISTIO_VERSION:-}" ]; then
  download_args+=(--istio-version "${ISTIO_VERSION}")
fi
"${REPO_DIR}/hack/istio/download-istio.sh" "${download_args[@]}"
ISTIO_DIR="$(ls -dt1 "${POC_OUT}/istio"/istio-* 2>/dev/null | head -n1)"
if [ -z "${ISTIO_DIR}" ] || [ ! -d "${ISTIO_DIR}" ]; then
  errormsg "Could not find downloaded Istio directory under ${POC_OUT}/istio"
  exit 1
fi
infomsg "Using Istio dir [${ISTIO_DIR}]"

# Namespace + traces-only Telemetry before workloads so Istio request metrics
# are never enabled for Bookinfo.
infomsg "Applying bookinfo Telemetry (100% tracing, Istio metrics disabled)"
${CLIENT_EXE} apply -f "${POC_DIR}/bookinfo-telemetry.yaml"

"${REPO_DIR}/hack/istio/install-bookinfo-demo.sh" \
  --client-exe kubectl \
  --istio-dir "${ISTIO_DIR}" \
  --namespace bookinfo \
  --auto-injection true \
  --traffic-generator \
  --wait-timeout 300s

# The shared installer intentionally excludes its generator from the mesh. Inject
# only this PoC's copy so the source side of the first trace edge is observable.
infomsg "Enabling sidecar injection on kiali-traffic-generator"
${CLIENT_EXE} -n bookinfo patch replicaset kiali-traffic-generator --type=merge \
  -p '{"spec":{"template":{"metadata":{"annotations":{"sidecar.istio.io/inject":"true"}}}}}'
${CLIENT_EXE} -n bookinfo delete pod -l app=kiali-traffic-generator --wait=true
${CLIENT_EXE} -n bookinfo wait pod -l app=kiali-traffic-generator --for=condition=Ready --timeout=300s

for app in details-v1 ratings-v1 reviews-v1 reviews-v2 reviews-v3 productpage-v1; do
  ${CLIENT_EXE} -n bookinfo rollout status "deployment/${app}" --timeout=300s
done

# Drop counters from any prior run before measuring Bookinfo.
${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" rollout restart deployment/otel-collector
${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" rollout status deployment/otel-collector --timeout=180s

infomsg "Bookinfo ready."
