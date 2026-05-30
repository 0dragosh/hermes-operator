#!/usr/bin/env bash
set -euo pipefail

YQ="${YQ:-./bin/yq}"
ROLE=config/rbac/role.yaml
HELM_CLUSTERROLE=hermes-operator

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

# Render the chart, extract the manager ClusterRole rules, compare against the kubebuilder-generated role.
generated=$("$YQ" eval '.rules' "$ROLE" | "$YQ" eval -P "$normalize_rules" -)
rendered=$(helm template hermes-operator charts/hermes-operator | "$YQ" eval "select(.kind == \"ClusterRole\" and .metadata.name == \"$HELM_CLUSTERROLE\") | .rules" - | "$YQ" eval -P "$normalize_rules" -)

if [ "$generated" != "$rendered" ]; then
    echo "::error::Helm chart ClusterRole drifted from kubebuilder-generated role." >&2
    diff <(echo "$generated") <(echo "$rendered") >&2 || true
    exit 1
fi
