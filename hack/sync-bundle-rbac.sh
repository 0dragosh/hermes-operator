#!/usr/bin/env bash
set -euo pipefail

# Compare/sync the rules in bundle/manifests/...clusterserviceversion.yaml
# against the kubebuilder-generated ClusterRole. Compares semantically (parses
# both sides through yq and compares the normalized rules array), so YAML
# style differences in the rest of the CSV don't produce false positives.
#
#   sync-bundle-rbac.sh           write the generated rules into the CSV
#   sync-bundle-rbac.sh --check   exit non-zero if the rules don't match

CSV=bundle/manifests/hermes-operator.clusterserviceversion.yaml
ROLE=config/rbac/role.yaml
YQ="${YQ:-./bin/yq}"

if [ ! -f "$ROLE" ]; then
  echo "::error::$ROLE not found. Run 'make manifests' first." >&2
  exit 1
fi

if ! YQ_VERSION_OUTPUT=$("$YQ" --version 2>/dev/null); then
  echo "::error::$YQ is required. Run 'make yq' or set YQ to mikefarah/yq v4." >&2
  exit 1
fi

if [[ "$YQ_VERSION_OUTPUT" != *"mikefarah/yq"* || "$YQ_VERSION_OUTPUT" != *"version v4."* ]]; then
  echo "::error::$YQ must be mikefarah/yq v4. Found: $YQ_VERSION_OUTPUT" >&2
  exit 1
fi

normalize_rules='
  . // []
  | map({
      "apiGroups": (.apiGroups // [] | sort),
      "resources": (.resources // [] | sort),
      "verbs": (.verbs // [] | sort),
      "resourceNames": (.resourceNames // [] | sort),
      "nonResourceURLs": (.nonResourceURLs // [] | sort)
    })
  | sort_by(.apiGroups, .resources, .verbs)
'

# Normalize both sides through yq -P (pretty) so we compare canonical forms.
GENERATED=$("$YQ" eval '.rules' "$ROLE" | "$YQ" eval -P "$normalize_rules" -)
EMBEDDED=$("$YQ" eval '.spec.install.spec.clusterPermissions[0].rules' "$CSV" | "$YQ" eval -P "$normalize_rules" -)

if [ "${1:-}" = "--check" ]; then
  if [ "$GENERATED" != "$EMBEDDED" ]; then
    echo "::error::Bundle CSV RBAC drifted from $ROLE. Run 'make sync-bundle-rbac' locally." >&2
    diff <(echo "$GENERATED") <(echo "$EMBEDDED") >&2 || true
    exit 1
  fi
  echo "Bundle CSV RBAC matches $ROLE."
  exit 0
fi

# Mutate mode: replace the rules array in place.
TMP=$(mktemp)
"$YQ" eval \
  '.spec.install.spec.clusterPermissions[0].rules = load("'"$ROLE"'").rules' \
  "$CSV" > "$TMP"
mv "$TMP" "$CSV"

echo "Bundle CSV RBAC synced from $ROLE."
