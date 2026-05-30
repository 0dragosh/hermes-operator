#!/usr/bin/env bash
set -euo pipefail

: "${VERSION:?set VERSION, for example VERSION=0.1.8}"
: "${IMG:?set IMG, for example IMG=ghcr.io/paperclipinc/hermes-operator:v0.1.8}"
: "${AGENT_IMG:=ghcr.io/paperclipinc/hermes-agent:v2026.5.29.2}"
: "${HONCHO_IMG:=ghcr.io/plastic-labs/honcho:0.1.0}"

if [[ "$AGENT_IMG" == *":latest" || "$AGENT_IMG" == *":latest@"* ]]; then
  echo "AGENT_IMG must be pinned and cannot use the mutable latest tag" >&2
  exit 1
fi
if [[ "$HONCHO_IMG" == *":latest" || "$HONCHO_IMG" == *":latest@"* ]]; then
  echo "HONCHO_IMG must be pinned and cannot use the mutable latest tag" >&2
  exit 1
fi

version="${VERSION#v}"
csv="bundle/manifests/hermes-operator.clusterserviceversion.yaml"
yq_bin="${YQ:-bin/yq}"
if [ ! -x "$yq_bin" ]; then
  yq_bin="$(command -v yq || true)"
fi
if [ -z "$yq_bin" ]; then
  echo "missing yq; run make yq or set YQ=/path/to/yq" >&2
  exit 127
fi

webhooks_file="$(mktemp)"
alm_examples_file="$(mktemp)"
trap 'rm -f "$webhooks_file" "$alm_examples_file"' EXIT

cat > "$webhooks_file" <<'YAML'
- type: MutatingAdmissionWebhook
  admissionReviewVersions:
  - v1
  containerPort: 9443
  targetPort: 9443
  deploymentName: hermes-operator-controller-manager
  failurePolicy: Fail
  generateName: mhermesinstance.hermes.agent
  sideEffects: None
  webhookPath: /mutate-hermes-agent-v1-hermesinstance
  rules:
  - apiGroups:
    - hermes.agent
    apiVersions:
    - v1
    operations:
    - CREATE
    - UPDATE
    resources:
    - hermesinstances
- type: ValidatingAdmissionWebhook
  admissionReviewVersions:
  - v1
  containerPort: 9443
  targetPort: 9443
  deploymentName: hermes-operator-controller-manager
  failurePolicy: Fail
  generateName: vhermesinstance.hermes.agent
  sideEffects: None
  webhookPath: /validate-hermes-agent-v1-hermesinstance
  rules:
  - apiGroups:
    - hermes.agent
    apiVersions:
    - v1
    operations:
    - CREATE
    - UPDATE
    resources:
    - hermesinstances
- type: ValidatingAdmissionWebhook
  admissionReviewVersions:
  - v1
  containerPort: 9443
  targetPort: 9443
  deploymentName: hermes-operator-controller-manager
  failurePolicy: Fail
  generateName: vhermesclusterdefaults.hermes.agent
  sideEffects: None
  webhookPath: /validate-hermes-agent-v1-hermesclusterdefaults
  rules:
  - apiGroups:
    - hermes.agent
    apiVersions:
    - v1
    operations:
    - CREATE
    - UPDATE
    resources:
    - hermesclusterdefaults
- type: ValidatingAdmissionWebhook
  admissionReviewVersions:
  - v1
  containerPort: 9443
  targetPort: 9443
  deploymentName: hermes-operator-controller-manager
  failurePolicy: Fail
  generateName: vhermesselfconfig.hermes.agent
  sideEffects: None
  webhookPath: /validate-hermes-agent-v1-hermesselfconfig
  rules:
  - apiGroups:
    - hermes.agent
    apiVersions:
    - v1
    operations:
    - CREATE
    - UPDATE
    resources:
    - hermesselfconfigs
YAML

cat > "$alm_examples_file" <<JSON
[
  {
    "apiVersion": "hermes.agent/v1",
    "kind": "HermesInstance",
    "metadata": { "name": "demo", "namespace": "default" },
    "spec": {
      "image": { "repository": "ghcr.io/paperclipinc/hermes-agent", "tag": "v2026.5.29.2" },
      "storage": { "persistence": { "enabled": true, "size": "10Gi" } },
      "envFrom": [{ "secretRef": { "name": "hermes-api-keys" } }]
    }
  },
  {
    "apiVersion": "hermes.agent/v1",
    "kind": "HermesInstance",
    "metadata": { "name": "production", "namespace": "hermes" },
    "spec": {
      "image": { "repository": "ghcr.io/paperclipinc/hermes-agent", "tag": "v2026.5.29.2" },
      "resources": {
        "requests": { "cpu": "500m", "memory": "1Gi" },
        "limits": { "cpu": "2", "memory": "4Gi" }
      },
      "storage": { "persistence": { "enabled": true, "size": "50Gi" } },
      "gateways": {
        "telegram": { "enabled": true, "botTokenSecretRef": { "name": "tg-token", "key": "token" } },
        "slack": {
          "enabled": true,
          "botTokenSecretRef": { "name": "slack-bot-token", "key": "token" },
          "appTokenSecretRef": { "name": "slack-app-token", "key": "token" },
          "signingSecretRef": { "name": "slack-signing-secret", "key": "secret" }
        }
      },
      "profileStore": {
        "honcho": {
          "enabled": true,
          "image": { "repository": "ghcr.io/plastic-labs/honcho", "tag": "0.1.0" },
          "persistence": { "enabled": true, "size": "5Gi" },
          "apiKeySecretRef": { "name": "hermes-honcho", "key": "apiKey" }
        }
      },
      "selfConfigure": {
        "enabled": true,
        "allowedActions": ["skills", "config", "envVars", "workspaceFiles", "profiles"],
        "protectedKeys": ["spec.image.*", "spec.storage.*", "spec.security.*", "spec.networking.*"]
      },
      "observability": { "serviceMonitor": { "enabled": true } },
      "backup": {
        "s3": {
          "bucket": "hermes-backups",
          "endpoint": "s3.amazonaws.com",
          "region": "us-east-1",
          "credentialsSecretRef": { "name": "hermes-s3-creds" }
        },
        "schedule": "0 3 * * *",
        "onDelete": true,
        "historyLimit": 30
      },
      "autoUpdate": {
        "enabled": true,
        "source": { "registry": "ghcr.io/paperclipinc/hermes-agent", "channel": "v2026.5" },
        "rollback": { "enabled": true, "probeFailureThreshold": 3 }
      }
    }
  },
  {
    "apiVersion": "hermes.agent/v1",
    "kind": "HermesSelfConfig",
    "metadata": { "name": "install-finance-skill", "namespace": "default" },
    "spec": {
      "instanceRef": "production",
      "addSkills": [{ "source": "git+https://github.com/foo/finance-skill@v1.2.0" }],
      "patchConfig": { "schedules": { "morning-brief": "0 8 * * *" } },
      "addEnvVars": [{ "name": "FINANCE_TZ", "value": "Europe/Berlin" }]
    }
  },
  {
    "apiVersion": "hermes.agent/v1",
    "kind": "HermesClusterDefaults",
    "metadata": { "name": "cluster" },
    "spec": {
      "image": { "repository": "ghcr.io/paperclipinc/hermes-agent", "tag": "v2026.5.29.2" },
      "storage": { "persistence": { "storageClassName": "gp3", "size": "10Gi" } },
      "observability": { "serviceMonitor": { "enabled": true } },
      "networking": { "networkPolicy": { "enabled": true } }
    }
  }
]
JSON

export BUNDLE_VERSION="$version"
export BUNDLE_NAME="hermes-operator.v${version}"
export BUNDLE_IMAGE="$IMG"
export BUNDLE_AGENT_IMAGE="$AGENT_IMG"
export BUNDLE_HONCHO_IMAGE="$HONCHO_IMG"
export WEBHOOKS_FILE="$webhooks_file"
export ALM_EXAMPLES_FILE="$alm_examples_file"

"$yq_bin" e -i '
  .metadata.name = strenv(BUNDLE_NAME) |
  .metadata.annotations.containerImage = strenv(BUNDLE_IMAGE) |
  .metadata.annotations."alm-examples" = load_str(strenv(ALM_EXAMPLES_FILE)) |
  .spec.version = strenv(BUNDLE_VERSION) |
  .spec.relatedImages = [
    {"name": "hermes-operator", "image": strenv(BUNDLE_IMAGE)},
    {"name": "hermes-agent", "image": strenv(BUNDLE_AGENT_IMAGE)},
    {"name": "honcho", "image": strenv(BUNDLE_HONCHO_IMAGE)}
  ] |
  (.spec.customresourcedefinitions.owned[] | select(.kind == "HermesInstance").specDescriptors[] | select(.displayName == "Honcho Profile Store").path) = "profileStore.honcho.enabled" |
  (.spec.customresourcedefinitions.owned[] | select(.kind == "HermesClusterDefaults").specDescriptors[] | select(.displayName == "Default StorageClass").path) = "storage.persistence.storageClassName" |
  (.spec.install.spec.deployments[] | select(.name == "hermes-operator-controller-manager").spec.template.spec.containers[] | select(.name == "manager").image) = strenv(BUNDLE_IMAGE) |
  (.spec.install.spec.deployments[] | select(.name == "hermes-operator-controller-manager").spec.template.spec.containers[] | select(.name == "manager").args) = [
    "--leader-elect",
    "--health-probe-bind-address=:8081",
    "--metrics-bind-address=:8443",
    "--metrics-secure=true"
  ] |
  (.spec.install.spec.deployments[] | select(.name == "hermes-operator-controller-manager").spec.template.spec.containers[] | select(.name == "manager").ports) = [
    {"name": "metrics", "containerPort": 8443, "protocol": "TCP"},
    {"name": "webhook", "containerPort": 9443, "protocol": "TCP"}
  ] |
  (.spec.install.spec.deployments[] | select(.name == "hermes-operator-controller-manager").spec.template.spec.containers[] | select(.name == "manager").livenessProbe) = {
    "httpGet": {"path": "/healthz", "port": 8081},
    "initialDelaySeconds": 15,
    "periodSeconds": 20
  } |
  (.spec.install.spec.deployments[] | select(.name == "hermes-operator-controller-manager").spec.template.spec.containers[] | select(.name == "manager").readinessProbe) = {
    "httpGet": {"path": "/readyz", "port": 8081},
    "initialDelaySeconds": 5,
    "periodSeconds": 10
  } |
  .spec.install.spec.webhookdefinitions = load(strenv(WEBHOOKS_FILE))
' "$csv"

echo "Updated $csv for hermes-operator.v${version} using $IMG and $AGENT_IMG"
