# Hermes Operator: Status Condition Catalogue

> Every condition the operator emits, what it means, what reason codes go with
> it, and how to debug it. This catalogue is part of the v1 stability contract
> (`docs/api-versioning.md` §"Status condition catalogue"). Conditions are
> additive across v1.x; reason codes are stable; both can be relied on by
> dashboards and consumers.

## How to read this catalogue

- Conditions follow the [Kubernetes meta/v1 Condition shape](https://kubernetes.io/docs/reference/using-api/api-concepts/#typical-status-properties):
  `type`, `status` (`True`/`False`/`Unknown`), `reason` (single PascalCase
  token), `message` (human-readable), `lastTransitionTime`, and
  `observedGeneration`.
- The aggregate `Ready` condition (HermesInstance only) is computed from the
  subsystem conditions: `Ready=True` iff every subsystem the spec activates
  reports `True`. The exact formula is in the "Aggregate Ready" subsection
  below.
- `(absent)` in a table means the condition is not set at all (the feature
  is not configured). Consumers MUST treat absence as "not applicable", not
  as failure.
- Reason codes are public API. Renames follow `docs/deprecations.md`.

---

## `HermesInstance` (`hermes.agent/v1`, short `hi`)

### Aggregate `Ready`

`Ready` is the rollup condition surfaced in the printer column `READY`. It
is computed at the end of every reconcile:

| Status | Reason | When |
|---|---|---|
| True | `AllSubsystemsReady` | Every required condition for the active spec reports `True`. The base set is `ConfigReady`, `SecretsReady`, `ServiceReady`, and `StatefulSetReady`, plus `StorageReady`, `NetworkPolicyReady`, and `RBACReady` when their default-enabled features are active. Optional PDB, HPA, Ingress, ServiceMonitor, PrometheusRule, ProfileStore, Backup, Restore, Migration, and AutoUpdate conditions suppress `Ready` whenever their corresponding feature is configured or in flight. |
| False | `SubsystemsPending` | At least one required subsystem condition is missing or `False`. `message` lists the failing subsystems comma-separated. |
| False | `SubsystemError` | A reconcile step failed before all resources could be applied. `message` names the failed subsystem and includes the controller error. |
| False | `Suspended` | `spec.suspended=true`. The instance is intentionally scaled to zero; Ready is `False` so dashboards page on accidental suspension, not on intentional ones. The `message` says "Suspended by spec.suspended=true". |

Troubleshooting: `kubectl describe hi <name>` shows every subsystem condition.
The failing ones drive the `message` of `Ready`.

### `StorageReady`

The data volume backing `~/.hermes` is reconciled. The operator creates a PVC
unless persistence is disabled, in which case the pod uses `emptyDir`.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | The operator-created PVC exists or was created for this reconcile. |
| True | `Disabled` | `spec.storage.persistence.enabled=false`. The StatefulSet uses an `emptyDir` data volume and no data PVC is created. |
| False | `Error` | Creating or reading the PVC failed. The `message` carries the API error. |

Troubleshooting: `kubectl get pvc -l app.kubernetes.io/instance=<name>` and check
`kubectl describe pvc <pvc>`.

### `ConfigReady`

The agent's `~/.hermes/config.yaml` ConfigMap is built and reflects the spec.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | The operator-owned agent config and workspace seed ConfigMaps were reconciled. Referenced `spec.config.configMapRef` and `spec.workspace.configMapRef` objects resolved. |
| False | `Error` | The controller could not resolve `spec.config.configMapRef` or `spec.workspace.configMapRef`, or failed to reconcile one of the generated ConfigMaps. The `message` carries the API or merge error. |

Troubleshooting: `kubectl get cm <name>-config -o yaml` shows the generated
config. `kubectl describe hi <name>` shows the merge error if any.

### `SecretsReady`

All Secret references in the spec resolve.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | The operator-owned gateway token Secret exists and has the generated data shape. |
| False | `Error` | Creating or updating the Secret failed. The `message` carries the API error. |

Troubleshooting: `kubectl get secret <name>` and `kubectl auth can-i get secret/<name> --as=system:serviceaccount:<ns>:hermes-operator`.

### `ServiceReady`

The Service exposing the agent gateway is in place.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | The Service exists and matches the desired ports, labels, and annotations. |
| False | `Error` | Creating or updating the Service failed. The `message` carries the API error. |

### `StatefulSetReady`

The agent StatefulSet exists and reports ready replicas.

| Status | Reason | When |
|---|---|---|
| True | `Ready` | `status.readyReplicas == status.replicas` and `status.replicas > 0`. |
| False | `StatefulSetNotReady` | The StatefulSet exists, but not every replica is ready yet. The `message` includes the ready and desired replica counts. |
| False | `Suspended` | `spec.suspended=true`; the StatefulSet is scaled to zero intentionally. |

### `NetworkPolicyReady`

The default-deny + allow-list NetworkPolicy is in place.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | The NetworkPolicy named `<instance>` exists, has owner-ref pointing at the instance, and matches the spec: deny-all baseline, DNS egress when allowed, `spec.security.networkPolicy.allowedEgressCIDRs`, `spec.security.networkPolicy.additionalEgress`, and the Honcho sibling rule when enabled. Gateway upstream egress is not derived from `spec.gateways`. |
| True | `Disabled` | `spec.security.networkPolicy.enabled=false`; any operator-owned NetworkPolicy is deleted. |
| False | `Error` | Creating, updating, or deleting the NetworkPolicy failed. |

Troubleshooting: `kubectl get netpol -n <ns>` and verify the CNI supports
NetworkPolicy.

### `RBACReady`

The agent's ServiceAccount, Role, and RoleBinding are in place when
operator-created RBAC is enabled.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | The ServiceAccount, Role, and RoleBinding exist when operator-created RBAC is enabled. |
| True | `Disabled` | `spec.security.rbac.createServiceAccount=false`; the operator does not create RBAC resources and the pod uses `spec.security.rbac.serviceAccountName` when set. |
| False | `Error` | Creating or updating the ServiceAccount, Role, or RoleBinding failed. |

Troubleshooting: `kubectl get sa,role,rolebinding -l app.kubernetes.io/instance=<name>`.

### `PDBReady`

The optional PodDisruptionBudget is in place.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | `spec.availability.podDisruptionBudget.enabled=true` and the PDB exists with the desired spec. |
| True | `Disabled` | The PDB feature is disabled and any operator-owned PDB is deleted. |
| False | `Error` | Creating, updating, or deleting the PDB failed. |

### `HPAReady`

The optional HorizontalPodAutoscaler is in place.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | `spec.availability.horizontalPodAutoscaler.enabled=true` and the HPA exists with the desired metrics and behavior. |
| True | `Disabled` | The HPA feature is disabled and any operator-owned HPA is deleted. |
| False | `Error` | Creating, updating, or deleting the HPA failed. |

### `IngressReady`

The optional Ingress is in place.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | `spec.networking.ingress.enabled=true` and the Ingress exists with the desired rules. |
| True | `Disabled` | The Ingress feature is disabled and any operator-owned Ingress is deleted. |
| False | `Error` | Creating, updating, or deleting the Ingress failed. |

### `ServiceMonitorReady`

The optional Prometheus Operator ServiceMonitor is in place.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | The Prometheus Operator CRD is installed, `spec.observability.serviceMonitor.enabled=true`, and the ServiceMonitor exists. |
| True | `Disabled` | The feature is disabled or the ServiceMonitor CRD is not installed. |
| False | `Error` | Creating, updating, or deleting the ServiceMonitor failed. |

### `PrometheusRuleReady`

The optional Prometheus Operator PrometheusRule is in place.

| Status | Reason | When |
|---|---|---|
| True | `Reconciled` | The Prometheus Operator CRD is installed, `spec.observability.prometheusRule.enabled=true`, and the PrometheusRule exists. |
| True | `Disabled` | The feature is disabled or the PrometheusRule CRD is not installed. |
| False | `Error` | Creating, updating, or deleting the PrometheusRule failed. |

### `ProfileStoreReady`

Honcho profile-store companion deployment.

| Status | Reason | When |
|---|---|---|
| True | `Disabled` | `spec.profileStore.honcho.enabled=false`; Honcho Deployment and Service are deleted while the Honcho PVC is retained for data safety. |
| True | `Ready` | The Honcho Deployment has at least one ready replica. |
| False | `DeploymentNotReady` | The Honcho Deployment is missing or has no ready replicas yet. The `message` includes the observed replica counts or API error. |
| False | `Error` | Creating, updating, or deleting Honcho resources failed. |

Troubleshooting: `kubectl get deploy,svc,pvc,secret -l app.kubernetes.io/instance=<name>,app.kubernetes.io/component=honcho`.

### `BackupReady`

State of scheduled backups (from Plan 5, restated here for the catalogue).

| Status | Reason | When |
|---|---|---|
| True | `Scheduled` | `spec.backup.schedule` is set, `spec.backup.s3` is configured, persistence is enabled, and the backup and prune CronJobs are reconciled. |
| True | `OnDeleteConfigured` | `spec.backup.onDelete=true`, `spec.backup.s3` is configured, and persistence is enabled. No scheduled CronJob is required. |
| False | `S3NotConfigured` | Scheduled backups or final backup on delete are enabled, but `spec.backup.s3` is unset. |
| False | `PersistenceDisabled` | Scheduled backups or final backup on delete are enabled, but `spec.storage.persistence.enabled=false`. Backup CronJobs are deleted. |
| (absent) |: | Neither `spec.backup.schedule` nor `spec.backup.onDelete` is set. |

Troubleshooting: `kubectl get cj,job -l app.kubernetes.io/instance=<name>,backup=true` and `kubectl logs job/<last-run>`.

### `RestoreApplied`

Terminal: once `True`, immutable for the lifetime of the instance.

| Status | Reason | When |
|---|---|---|
| True | `RestoreCompleted` | `status.restoredFrom == spec.restoreFrom`. |
| False | `WaitingForPod` | `spec.restoreFrom` is set, but the StatefulSet pod is not visible yet. The controller requeues quickly. |
| False | `Restoring` | The `init-restore` init container is in progress. |
| False | `RestoreFailed` | The `init-restore` init container exited non-zero. The `message` includes the exit code and the container termination message. |
| (absent) |: | `spec.restoreFrom` is unset. |

Troubleshooting: `kubectl logs <instance>-0 -c init-restore` and inspect the snapshot key in S3.

### `AutoUpdated`

Outcome of the most recent auto-update cycle.

| Status | Reason | When |
|---|---|---|
| True | `UpToDate` | The current tag is the highest in `spec.autoUpdate.source.channel`. No rollout needed. |
| True | `Confirmed` | A rollout completed and passed the readiness watch window. `status.autoUpdate.lastSuccessTag` is populated. |
| False | `RolloutInFlight` | A rollout is currently being watched. `status.autoUpdate.targetTag` carries the candidate. |
| False | `RolledBack` | The most recent rollout failed; image reverted. The `message` references the failed tag. |
| False | `NoMatchingTag` | No tag in the registry matches the channel pattern. |
| False | `SuppressedKnownFailure` | The highest matching tag equals `status.autoUpdate.lastFailedTag`: auto-update declines to retry a tag that has already failed. Manual intervention (clear `lastFailedTag` via subresource patch) is required. |
| (absent) |: | `spec.autoUpdate.enabled=false`. |

Troubleshooting: `kubectl get hi <name> -o jsonpath='{.status.autoUpdate}'` for the full sub-status.

### `AutoUpdateRolledBack`

Present only after a rollback. The reason embeds the failed tag.

| Status | Reason | When |
|---|---|---|
| True | `RolledBackFrom_<tag>` | A rollback completed. The message describes why (deadline elapsed or `probeFailureThreshold` reached). |

The condition is removed on the next successful `AutoUpdated=True` (reason=`Confirmed`) cycle, so it acts as a one-shot signal. Dashboards typically alarm on the transition `(absent) → True`.

### `MigrationCompleted`

Terminal: once `True`, immutable for the lifetime of the instance.

| Status | Reason | When |
|---|---|---|
| True | `MigrationCompleted` | The `init-migrate-from-openclaw` init container exited 0. |
| False | `WaitingForPod` | `spec.migration.fromOpenClaw` is set, but the StatefulSet pod is not visible yet. The controller requeues quickly. |
| False | `Migrating` | The migration init container has not terminated yet. |
| False | `MigrationFailed` | The migration init container exited non-zero. The `message` includes the exit code and the container termination message. |
| (absent) |: | `spec.migration.fromOpenClaw` is unset. |

Troubleshooting: `kubectl logs <instance>-0 -c init-migrate-from-openclaw`.

---

## `HermesSelfConfig` (`hermes.agent/v1`, short `hsc`)

Phase derives from these conditions: `Applied → Applied`, `Denied → Denied`, otherwise `Pending`.

### `Applied`

| Status | Reason | When |
|---|---|---|
| True | `SSASuccess` | The SSA patch against the parent `HermesInstance` (and workspace ConfigMap, for `addWorkspaceFiles`) completed without an SSA conflict. `status.appliedAt` and `status.appliedFields` are populated. |
| False | (transient) | Transitioning. The next reconcile will move to `Applied=True` or `Denied=True`. |

### `Denied`

| Status | Reason | When |
|---|---|---|
| True | `PolicyViolation` | The request touched a path on the parent instance's `selfConfigure.protectedKeys` allowlist, or `selfConfigure.enabled=false`. `status.denyReason` carries the human-readable detail. The operator also emits a `Warning` Event with reason `PolicyViolation` on the parent instance so `kubectl describe hi` shows it. |
| True | `InstanceNotFound` | `spec.instanceRef` refers to a `HermesInstance` that does not exist in the namespace. |
| True | `InstanceTerminating` | The parent instance has a `deletionTimestamp` set. |
| True | `SSAConflict` | The SSA patch lost a field-ownership conflict to a different field manager. `denyReason` lists the conflicting path and the other manager's name (e.g. `kustomize-controller`). The user typically resolves this by changing the SelfConfig to a different field or by force-taking ownership manually. |

### `Pending`

| Status | Reason | When |
|---|---|---|
| True | `AwaitingInstanceReady` | The parent instance is not yet `Ready=True`. The SelfConfig reconciler defers application until it is. Prevents racing the initial bring-up. |
| True | `RateLimited` | More than 5 SelfConfigs per minute for the same instance: back off. Reset after the burst window passes. |

Troubleshooting: `kubectl get hsc -n <ns>` shows the phase. `kubectl describe hsc <name>` shows `status.denyReason` or the conditions detail.

---

## `HermesClusterDefaults` (`hermes.agent/v1`, short `hcd`, cluster-scoped singleton `cluster`)

### `Active`

| Status | Reason | When |
|---|---|---|
| True | `Applied` | The singleton `cluster` exists, passes validation, and the defaulting webhook is using it on every admission. `status.observedGeneration == metadata.generation`. |
| False | `WrongName` | A `HermesClusterDefaults` exists with a name other than `cluster`. The validating webhook rejects new ones; this condition exists for legacy resources created before the webhook was installed. |
| (absent) |: | No `HermesClusterDefaults` exists in the cluster. The defaulter falls back to its built-in fallback defaults. |

### `Invalid`

| Status | Reason | When |
|---|---|---|
| True | `SchemaViolation` | A field on the singleton fails server-side validation (e.g. negative quantity, malformed cron). The defaulter ignores invalid fields and uses fallback values for them: the rest of the singleton still applies. `message` lists the offending JSON paths. |
| True | `ImagePullSecretMissing` | `spec.registry.pullSecretName` does not resolve in the operator's namespace. Defaulter skips that field. |

The two conditions can both be `True` simultaneously: `Active=True` (defaults are applied) and `Invalid=True` (some fields are skipped). Dashboards key on `Invalid` for alerting.

---

## Reason-code naming convention

For consistency across CRs and to make grep work in dashboards:

- **PascalCase**, no spaces, no slashes (allow underscore only for value-carrying reasons like `RolledBackFrom_<tag>`).
- **One token per cause.** "ScheduledAndS3Configured" is wrong; the reason is `Scheduled`, and the configured backend is implicit.
- **Reasons are added, never repurposed.** Adding `S3EndpointInvalid` as a new reason for `BackupReady=False` is non-breaking. Changing what `S3NotConfigured` means is breaking.
- **Reasons that embed values** use `_` as the separator (the only allowed underscore use).
