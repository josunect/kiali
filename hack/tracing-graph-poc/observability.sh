#!/bin/bash

##############################################################################
# observability.sh
#
# Installs Sail (Istio + prometheus/jaeger addons), the PoC OTel Collector,
# patches the Istio Prometheus addon with service-graph scrape + recording
# rules, and installs Kiali via the server Helm chart.
##############################################################################

set -euo pipefail

# shellcheck source=hack/tracing-graph-poc/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

patch_prometheus_addon() {
  infomsg "Patching Istio Prometheus addon (scrape otel-collector + extension recording rules)"

  local prom_yml rules_yml
  prom_yml="$(${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" get configmap prometheus \
    -o jsonpath='{.data.prometheus\.yml}')"
  if [ -z "${prom_yml}" ]; then
    errormsg "configmap/prometheus has no prometheus.yml"
    exit 1
  fi
  if ! printf '%s' "${prom_yml}" | grep -q 'rule_files'; then
    errormsg "configmap/prometheus prometheus.yml has no rule_files (unexpected addon layout)"
    exit 1
  fi

  # Append the PoC scrape job when missing (idempotent).
  if ! printf '%s' "${prom_yml}" | grep -q 'job_name: otel-service-graph'; then
    prom_yml="$(printf '%s\n' "${prom_yml}" | yq eval-all '
      select(fileIndex == 0) as $prom |
      select(fileIndex == 1) as $job |
      $prom |
      .scrape_configs += [$job]
    ' - "${POC_DIR}/prometheus-scrape.yaml")"
  fi

  # Prefer a shorter scrape/eval interval for the lab.
  # scrape_timeout must stay <= scrape_interval (addon default timeout is 10s).
  prom_yml="$(printf '%s' "${prom_yml}" | yq eval '
    .global.scrape_interval = "5s" |
    .global.scrape_timeout = "5s" |
    .global.evaluation_interval = "5s"
  ' -)"

  rules_yml="$(${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" get configmap prometheus \
    -o jsonpath='{.data.recording_rules\.yml}')"
  if [ -z "${rules_yml}" ] || [ "${rules_yml}" = "{}" ]; then
    rules_yml="groups: []"
  fi

  local prom_tmp rules_tmp poc_rules_tmp
  prom_tmp="$(mktemp)"
  rules_tmp="$(mktemp)"
  poc_rules_tmp="$(mktemp)"
  printf '%s' "${prom_yml}" > "${prom_tmp}"
  printf '%s' "${rules_yml}" > "${rules_tmp}"
  POC_MESH_CLUSTER="${POC_MESH_CLUSTER}" envsubst '${POC_MESH_CLUSTER}' \
    < "${POC_DIR}/rules.yaml" > "${poc_rules_tmp}"

  # fileIndex only advances across separate files, not --- documents on one stream.
  rules_yml="$(yq eval-all '
    select(fileIndex == 0) as $base |
    select(fileIndex == 1) as $poc |
    ($base | .groups // []) as $existing |
    ($poc.groups[0].name) as $name |
    $base |
    .groups = ([ $existing[] | select(.name != $name) ] + $poc.groups)
  ' "${rules_tmp}" "${poc_rules_tmp}")"
  printf '%s' "${rules_yml}" > "${rules_tmp}"

  ${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" patch configmap prometheus --type merge -p "$(
    jq -n --rawfile prom "${prom_tmp}" --rawfile rules "${rules_tmp}" \
      '{data: {"prometheus.yml": $prom, "recording_rules.yml": $rules}}'
  )"
  rm -f "${prom_tmp}" "${rules_tmp}" "${poc_rules_tmp}"

  ${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" rollout restart deployment/prometheus
  ${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" rollout status deployment/prometheus --timeout=180s
}

infomsg "Installing Istio via Sail (addons: prometheus jaeger)"
"${REPO_DIR}/hack/istio/install-istio-via-sail.sh" \
  -a "prometheus jaeger" \
  -pf "${POC_DIR}/istio-patch.yaml" \
  -w true

infomsg "Deploying OTel Collector (service_graph) into ${ISTIO_NAMESPACE}"
${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" create configmap otel-collector \
  --from-file="collector.yaml=${POC_DIR}/collector.yaml" \
  --dry-run=client -o yaml | ${CLIENT_EXE} apply -f -
${CLIENT_EXE} apply -f "${POC_DIR}/observability.yaml"
${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" rollout restart deployment/otel-collector 2>/dev/null || true
${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" rollout status deployment/otel-collector --timeout=180s

patch_prometheus_addon

infomsg "Installing Kiali (Helm kiali-server)"
if [ -n "${KIALI_CHART_DIR:-}" ]; then
  chart=("${KIALI_CHART_DIR}")
else
  chart=(kiali-server --repo https://kiali.org/helm-charts --version 2.33.0)
fi
helm upgrade --install kiali "${chart[@]}" -n "${ISTIO_NAMESPACE}" \
  -f "${POC_DIR}/kiali-values.yaml" --wait --timeout 180s

for app in otel-collector prometheus jaeger kiali; do
  ${CLIENT_EXE} -n "${ISTIO_NAMESPACE}" rollout status "deployment/${app}" --timeout=180s
done

infomsg "Observability stack ready in ${ISTIO_NAMESPACE}"
