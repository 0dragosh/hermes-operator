#!/usr/bin/env bash
set -euo pipefail

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 127
  fi
}

assert_contains() {
  local file="$1"
  local pattern="$2"
  local label="$3"
  if ! grep -Eq "$pattern" "$file"; then
    echo "missing ${label} in ${file}" >&2
    exit 1
  fi
}

require helm
require kubectl

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

helm_out="$tmpdir/helm.yaml"
kustomize_out="$tmpdir/kustomize.yaml"

helm template hermes-operator charts/hermes-operator > "$helm_out"
kubectl kustomize config/default > "$kustomize_out"

for rendered in "$helm_out" "$kustomize_out"; do
  assert_contains "$rendered" 'kind: (MutatingWebhookConfiguration|ValidatingWebhookConfiguration)' "webhook configuration"
  assert_contains "$rendered" 'kind: Certificate' "cert-manager Certificate"
  assert_contains "$rendered" 'kind: Issuer' "cert-manager Issuer"
  assert_contains "$rendered" 'metrics-bind-address' "manager metrics bind arg"
  assert_contains "$rendered" 'livenessProbe:' "liveness probe"
  assert_contains "$rendered" 'readinessProbe:' "readiness probe"
  assert_contains "$rendered" 'kind: Service' "Service"
  assert_contains "$rendered" 'tokenreviews|subjectaccessreviews' "metrics auth RBAC"
  assert_contains "$rendered" '/metrics' "metrics reader RBAC"
done

echo "distribution parity check passed"
