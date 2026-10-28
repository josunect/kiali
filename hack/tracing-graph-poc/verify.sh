#!/bin/bash

##############################################################################
# verify.sh
#
# Checks native service-graph edges, extension rates, Kiali graph shape,
# a complete Jaeger trace, ready sidecars, and absence of Istio request metrics.
# Requires run.sh access (port-forwards) to be running.
##############################################################################

set -euo pipefail

# shellcheck source=hack/tracing-graph-poc/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

EXPECTED='[["kiali-traffic-generator","productpage"],["productpage","details"],["productpage","reviews"],["reviews","ratings"]]'
SERVICES='["kiali-traffic-generator","productpage","details","reviews","ratings"]'
VERIFY_DIR="${POC_OUT}/verification"
mkdir -p "${VERIFY_DIR}"

query() {
  curl -fsS --max-time 15 --get http://localhost:19090/api/v1/query --data-urlencode "query=$1"
}

verify_once() {
  local now trace_id metrics app
  query 'traces_service_graph_request_total{client_service_namespace="bookinfo",server_service_namespace="bookinfo"}' \
    > "${VERIFY_DIR}/native.json" || return 1
  jq -e --argjson expected "${EXPECTED}" '
    .status == "success" and
    ([.data.result[].metric | [.client,.server]] | unique | sort) == ($expected | sort)
  ' "${VERIFY_DIR}/native.json" > /dev/null || return 1
  query 'sum by (source_name,dest_name,status_code) (rate(kiali_ext_requests_total{extension="otel-traces",source_namespace="bookinfo",status_code="200"}[60s]))' \
    > "${VERIFY_DIR}/rates.json" || return 1
  jq -e --argjson expected "${EXPECTED}" '
    .status == "success" and
    ([.data.result[] | select((.value[1]|tonumber)>0) | [.metric.source_name,.metric.dest_name]] | sort) == ($expected | sort)
  ' "${VERIFY_DIR}/rates.json" > /dev/null || return 1
  query 'count({__name__=~"istio_requests_total|istio_tcp_sent_bytes_total"})' \
    > "${VERIFY_DIR}/istio.json" || return 1
  jq -e '.status == "success" and (.data.result|length)==0' "${VERIFY_DIR}/istio.json" > /dev/null || return 1
  curl -fsS --max-time 15 --get http://localhost:21001/kiali/api/namespaces/graph \
    --data-urlencode namespaces=bookinfo --data-urlencode graphType=app \
    --data-urlencode duration=60s --data-urlencode injectServiceNodes=false \
    > "${VERIFY_DIR}/graph.json" || return 1
  jq -e --argjson expected "${EXPECTED}" --argjson services "${SERVICES}" '
    .elements as $g |
    ($g.nodes | map({key:.data.id,value:.data.service}) | from_entries) as $names |
    ([$g.nodes[].data.service] | sort) == ($services | sort) and
    all($g.nodes[].data; .isExtension.name=="otel-traces" and (.isInaccessible != true)) and
    ([$g.edges[].data | [$names[.source],$names[.target]]] | sort) == ($expected | sort) and
    all($g.edges[].data.traffic; (.rates.http|tonumber)>0 and .responses["200"]!=null)
  ' "${VERIFY_DIR}/graph.json" > /dev/null || return 1

  # Proxy exports flush independently; wait for all spans of older traces.
  now=$(date +%s)
  curl -fsS --max-time 15 --get http://localhost:17686/api/traces \
    --data-urlencode service=productpage --data-urlencode limit=20 \
    --data-urlencode "start=$(((now-300)*1000000))" --data-urlencode "end=$(((now-20)*1000000))" \
    > "${VERIFY_DIR}/traces.json" || return 1
  trace_id=$(jq -er --argjson services "${SERVICES}" '
    [.data[] | select(([$services[]] - [.processes[].serviceName] | length)==0 and (.spans|length)>=8)] |
    .[0].traceID // empty
  ' "${VERIFY_DIR}/traces.json") || return 1
  ${CLIENT_EXE} -n bookinfo get pods -o json > "${VERIFY_DIR}/pods.json" || return 1
  jq -e '
    [.items[] | select(.metadata.deletionTimestamp==null)] |
    length==7 and all(.[]; any(.status.containerStatuses[]?; .name=="istio-proxy" and .ready))
  ' "${VERIFY_DIR}/pods.json" > /dev/null || return 1
  for app in productpage-v1 details-v1 reviews-v1 reviews-v2 reviews-v3 ratings-v1; do
    metrics="${VERIFY_DIR}/${app}-proxy-metrics.txt"
    ${CLIENT_EXE} -n bookinfo exec "deployment/${app}" -c istio-proxy -- \
      pilot-agent request GET stats/prometheus > "${metrics}" 2> "${VERIFY_DIR}/proxy-check.log" || return 1
    grep -q '^envoy_' "${metrics}" || return 1
    if grep -q '^istio_requests_total' "${metrics}"; then
      errormsg "Istio traffic metrics are still enabled on ${app}"
      return 1
    fi
  done
  jq -n --arg verified_at "$(date -u +%FT%TZ)" --arg trace_id "${trace_id}" \
    --slurpfile native "${VERIFY_DIR}/native.json" --slurpfile rates "${VERIFY_DIR}/rates.json" \
    --slurpfile graph "${VERIFY_DIR}/graph.json" --slurpfile traces "${VERIFY_DIR}/traces.json" '
    {verified_at:$verified_at, native_metrics:$native[0], extension_rates:$rates[0],
     kiali_graph:$graph[0], complete_trace_id:$trace_id,
     complete_trace_spans:([$traces[0].data[] | select(.traceID==$trace_id) | .spans|length][0]),
     proxy_metrics_disabled:true, istio_traffic_metrics_present:false}
  ' > "${POC_OUT}/bookinfo-evidence.json" || return 1
  jq '{verified_at, complete_trace_id, complete_trace_spans, proxy_metrics_disabled,
       nodes:(.kiali_graph.elements.nodes|length), edges:(.kiali_graph.elements.edges|length)}' \
    "${POC_OUT}/bookinfo-evidence.json"
}

deadline=$((SECONDS+180))
until verify_once; do
  if ((SECONDS >= deadline)); then
    errormsg "Incomplete Bookinfo telemetry; inspect ${VERIFY_DIR}"
    exit 1
  fi
  echo "Waiting for paired spans, rates and complete Bookinfo traces..." >&2
  sleep 5
done
