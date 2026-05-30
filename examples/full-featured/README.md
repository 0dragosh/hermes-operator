# Full-featured `HermesInstance`

A deliberately maximal example: every top-level sub-spec is exercised at
least once. **Do not copy this into production as-is**: it is for
discovery. Start from [`minimal/`](../minimal/) and add only what you
need.

## Prerequisites

This example references several Secrets that you must create first:

```bash
kubectl create namespace agents

# Gateway tokens (placeholder values: replace with real ones).
kubectl create secret generic hermes-telegram \
  -n agents --from-literal=token=REPLACE_WITH_TELEGRAM_BOT_TOKEN
kubectl create secret generic hermes-discord \
  -n agents --from-literal=token=REPLACE_WITH_DISCORD_BOT_TOKEN

# S3 backup credentials.
kubectl create secret generic hermes-s3-creds \
  -n agents \
  --from-literal=S3_ACCESS_KEY_ID=REPLACE \
  --from-literal=S3_SECRET_ACCESS_KEY=REPLACE

# Honcho secret.
kubectl create secret generic hermes-honcho \
  -n agents --from-literal=apiKey=REPLACE
```

## Apply

```bash
kubectl apply -n agents -f hermesinstance.yaml
```

## What this exercises

| Sub-spec | What |
|---|---|
| `image` | Pinned tag + pull policy. |
| `config` | Raw inline + merge mode. |
| `workspace` | Two seeded files. |
| `resources` | Explicit requests + limits. |
| `security` | Pod + container security context, RBAC annotation (IRSA), NetworkPolicy with explicit egress. |
| `storage` | 50Gi GP3 PVC. |
| `networking` | Service + Ingress settings. |
| `observability` | Metrics + ServiceMonitor. |
| `availability` | PDB + HPA + topology spread. |
| `probes` | Custom liveness/readiness. |
| `backup` | Scheduled + on-delete + pre-update, with history limit. |
| `runtime` | Pinned Python + uv dependency checks + extra pip packages. |
| `gateways` | Telegram + Discord. |
| `profileStore` | Honcho with persistence. |
| `autoUpdate` | Channel-pinned with rollback. |
| `selfConfigure` | Enabled with a strict `protectedKeys`. |
| `scheduling` | Node selector + toleration. |
| `initContainers` | One custom init. |
| `sidecars` | One custom sidecar. |
| `extraVolumes` / `extraVolumeMounts` | Extra hostPath for tracing. |
| `envFrom` / `env` | A configMapRef + a literal env var. |
| `suspended` | Set to `false` (default): flip to `true` to scale to zero. |

The corresponding conditions on `kubectl describe hi full-featured` are:
`Ready`, `StorageReady`, `ConfigReady`, `SecretsReady`, `NetworkPolicyReady`,
`RBACReady`, `GatewayReady`, `ProfileStoreReady`, `BackupReady`,
`AutoUpdated`, `WebhookReady`. (`RestoreApplied`, `MigrationCompleted`, and
`AutoUpdateRolledBack` are absent because nothing triggers them.)
