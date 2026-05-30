# Hermes Operator Project Audit

Date: 2026-05-29

## Executive Summary

Hermes Operator is **not production ready** in its current state.

The project has a solid Kubebuilder-style foundation: API types are separated from controllers, resource builders are mostly pure functions, admission webhooks are isolated, and there is meaningful unit/envtest/conformance/e2e scaffolding. The main problem is feature maturity and ownership drift. Several advertised v1 features are broken, only partially implemented, unsafe, stale in documentation/distribution metadata, or not verified by live tests.

The highest-risk areas are:

- Runtime image packaging is incomplete.
- Auto-update conflicts with the main StatefulSet reconciler.
- Backup/restore/migration paths likely fail at runtime and do not match documentation.
- Shell-string construction allows command injection in some generated Jobs/init containers.
- Public API fields are accepted but ignored or only partially wired.
- Status/readiness can report success while important day-2 subcontrollers fail.
- Helm, OLM, kustomize, examples, and README are out of sync.
- RBAC and workload passthrough make `HermesInstance` creation a privileged operation.

## Scope And Method

The assessment covered:

- Production readiness
- Correctness and feature wiring
- Security posture
- Code quality, architecture, and maintainability
- Distribution artifacts: Helm, OLM bundle, kustomize, Dockerfiles, GitHub Actions
- Local static and test verification
- Guarded Kubernetes live smoke check

Four GPT-5.5 xhigh read-only subagents were used for the main assessment areas:

- Production readiness
- Implementation correctness and wiring
- Security
- Codebase quality and architecture

One additional GPT-5.5 xhigh subagent performed a guarded live Kubernetes smoke check.

## Verification Performed

Local commands:

- `CGO_ENABLED=0 go test ./api/... ./internal/resources/... ./internal/oci/... ./internal/webhook/...`
  - Passed.
- `CGO_ENABLED=0 go test -run '^$' ./...`
  - Passed compile-only across packages.
- Controller envtest suite:
  - Failed. The manager could not bind the default metrics listener on `:8080`, then multiple controller specs timed out waiting for resources.
- `kubectl kustomize config/default | rg WebhookConfiguration`
  - Returned no matches, confirming the plain-manifest output does not include webhook configurations.
- `git status --short`
  - Clean after the audit.

Live cluster smoke:

- `kubectl` was available.
- Cilium CRDs were present.
- A unique ephemeral namespace was created.
- A Cilium egress-deny policy was applied before any workload/CR checks.
- Hermes CRDs/operator/webhooks were absent, so no `HermesInstance` was created and no cluster-wide Hermes install was attempted.
- Cleanup succeeded; the ephemeral namespace was deleted.

## Production Readiness Findings

### Not Production Ready

The project has broad release and installation infrastructure, but several advertised production paths are broken or unproven.

### Agent Image Packaging Is Incomplete

`images/hermes-agent/uv.lock` is absent, but the agent image Dockerfile requires it:

- `images/hermes-agent/Dockerfile:47`

Related workflow code also expects that file:

- `.github/workflows/agent-image.yaml:60`

The smoke workflow explicitly skips when `uv.lock` is missing:

- `.github/workflows/agent-image-smoke.yaml:23`

Impact: the published agent image path is not reproducible from the committed repository, and CI can miss the failure.

### Backup, Restore, And Migration Jobs Are Not Runtime-Safe

Backup jobs use `mktemp`, `jq`, and `zstd` while running with `readOnlyRootFilesystem: true` and no writable `/tmp` mount:

- `internal/resources/backup_job.go:50`
- `internal/resources/backup_job.go:109`
- `internal/resources/backup_cronjob.go:64`

Restore requires `zstd`:

- `internal/resources/restore_init.go:41`

S3 migration requires `aws` and `zstd`:

- `internal/resources/migration_init.go:69`

The agent image installs ffmpeg/ripgrep/git/ssh/certs/tini but not the full backup/migration toolchain:

- `images/hermes-agent/Dockerfile:72`

Impact: backup/restore/migration may fail in production even when manifests reconcile.

### Backup Format Does Not Match Documentation

Documentation says backups are raw `.tar.zst` objects at deterministic S3 keys:

- `docs/backup-restore.md:9`
- `docs/backup-restore.md:36`
- `docs/backup-format.md:3`

The implementation streams the tarball into `restic backup --stdin`:

- `internal/resources/backup_job.go:49`
- `internal/resources/backup_cronjob.go:68`

Prune filters by tags that backup jobs never set:

- `internal/resources/backup_cronjob.go:208`

Impact: users following the documented S3 object model will not get the described behavior.

### Plain Manifest Distribution Omits Webhooks

`make installer` emits `config/default`, and GoReleaser ships that asset:

- `Makefile:176`
- `.goreleaser.yaml:11`

But webhook and cert-manager resources are commented out in:

- `config/default/kustomization.yaml:21`

Verified with:

```bash
kubectl kustomize config/default | rg WebhookConfiguration
```

Result: no webhook resources.

Impact: plain-manifest installs lack the validating/defaulting protections advertised in the README.

### OLM And Release Metadata Are Stale Or Malformed

The bundle CSV contains stale or malformed version/image values:

- `bundle/manifests/hermes-operator.clusterserviceversion.yaml:4`
- `bundle/manifests/hermes-operator.clusterserviceversion.yaml:240`
- `bundle/manifests/hermes-operator.clusterserviceversion.yaml:625`

Release-please appears configured to write raw versions into fields that need operator-style names/images:

- `release-please-config.json:25`

Impact: OperatorHub/OLM distribution is not reliable until regenerated and validated.

### Helm Chart Omits Some Production Manager Wiring

Helm passes metrics security and log-level args:

- `charts/hermes-operator/templates/deployment.yaml:25`

But the manager binary defaults `--metrics-bind-address` to disabled:

- `cmd/main.go:87`

Kustomize adds the metrics bind arg:

- `config/default/manager_metrics_patch.yaml:1`

Helm also lacks the delegated auth RBAC rendered by kustomize for secure metrics:

- `config/rbac/metrics_auth_role.yaml:9`

Impact: Helm manager metrics may be disabled or unusable despite chart values and exposed ports.

### Release Gates Are Too Weak

The release workflow publishes on tag:

- `.github/workflows/release.yaml:134`

Conformance runs separately on tags and scheduled runs:

- `.github/workflows/conformance.yaml:6`

PR conformance only runs for conformance workflow/test path changes:

- `.github/workflows/conformance.yaml:9`

Impact: release publishing is not gated by full conformance for normal code changes.

## Correctness And Wiring Findings

### Auto-Update Is Overwritten By Normal Reconciliation

Auto-update patches the StatefulSet image:

- `internal/controller/autoupdate.go:180`

The main reconciler later rebuilds and assigns `obj.Spec = desired.Spec`:

- `internal/controller/hermesinstance_controller.go:456`

The StatefulSet builder only uses `spec.image.tag`:

- `internal/resources/statefulset.go:277`

Impact: auto-update can be reset or thrashed by normal reconciliation.

### `storage.persistence.enabled=false` Is Ignored

The API exposes `spec.storage.persistence.enabled`:

- `api/v1/hermesinstance_types.go:182`

The controller always reconciles a PVC:

- `internal/controller/hermesinstance_controller.go:199`

The StatefulSet always mounts that PVC:

- `internal/resources/statefulset.go:185`

Docs claim disabled persistence uses `emptyDir`:

- `docs/conditions.md:53`

Impact: a public API field does not do what it says.

### Metrics Scraping Is Not Wired To The Agent Pod

The Service creates a `metrics` port:

- `internal/resources/service.go:76`

ServiceMonitor scrapes that port:

- `internal/resources/servicemonitor.go:55`

The agent container declares only the `gateway` port:

- `internal/resources/statefulset.go:62`

Impact: Service/ServiceMonitor metrics scraping is likely broken.

### Runtime `extraAptPackages` Cannot Affect The Main Container

The API says extra apt packages are installed before the agent starts:

- `api/v1/hermesinstance_types.go:1083`

Implementation installs them in an init container filesystem:

- `internal/resources/runtime_init.go:104`

Init container filesystem changes do not persist into the main container.

Impact: the feature cannot work as advertised.

### Honcho Persistence Disabled Produces An Unschedulable Deployment

The controller skips Honcho PVC creation when persistence is disabled:

- `internal/controller/hermesinstance_controller.go:481`

The Honcho Deployment always mounts the PVC:

- `internal/resources/honcho.go:117`

Impact: `profileStore.honcho.persistence.enabled=false` can produce an invalid workload.

### Workspace ConfigMap Reference Is Ignored

The API declares `spec.workspace.configMapRef`:

- `api/v1/hermesinstance_types.go:256`

Docs promise merge behavior:

- `docs/api-reference.md:75`

The workspace ConfigMap builder only uses `initialFiles` and `initialDirs`:

- `internal/resources/workspace_configmap.go:38`

Impact: referenced workspace ConfigMaps do nothing.

### `HermesSelfConfig.contentFrom` Is Ignored

The API declares `addWorkspaceFiles[].contentFrom`:

- `api/v1/hermesselfconfig_types.go:136`

Apply logic only copies literal `Content`:

- `internal/controller/selfconfig_apply.go:116`

Impact: a declared source mechanism is nonfunctional.

### SelfConfig Env Var Exclusivity Is Not Validated

The API documents `value` and `valueFrom` as mutually exclusive:

- `api/v1/hermesselfconfig_types.go:99`

The validator does not enforce that invariant:

- `internal/webhook/webhook_hermesselfconfig.go:78`

Impact: invalid or ambiguous SelfConfig resources can be admitted.

### Status Conditions Are Acknowledgements, Not Readiness

Subsystem conditions are set `True/Reconciled` after reconciliation calls:

- `internal/controller/hermesinstance_controller.go:119`

Overall `Ready` checks only StatefulSet replica readiness:

- `internal/controller/hermesinstance_controller.go:631`

Docs describe richer all-subsystem readiness and PVC checks:

- `docs/conditions.md:35`
- `docs/conditions.md:45`

Impact: status can say ready while important subsystems are not healthy.

### Day-2 Subcontroller Results Are Ignored

Backup, migration, restore, and auto-update errors are logged but not returned:

- `internal/controller/hermesinstance_controller.go:149`

Returned `ctrl.Result` requeues from those subcontrollers are also ignored.

Impact: failures and requested requeues can be lost.

### Examples Are Stale Against The API

Examples use unsupported or currently wrong fields such as:

- `imagePullPolicy`
- `imagePullSecrets`
- `workspace.files`
- `networking.networkPolicy`
- `availability.pdb`
- `runtime.extraApt`
- `profileStore.enabled`
- `webTerminal`
- `tailscale`

Example paths:

- `examples/full-featured/hermesinstance.yaml:6`
- `examples/cluster-defaults/clusterdefaults.yaml:6`

Impact: users copying examples may apply invalid or ignored configuration.

## Security Findings

### Shell Injection In Profile Snapshot Jobs

`profileID` is only length-validated:

- `api/v1/hermesselfconfig_types.go:142`

It is used raw in a filesystem path and embedded into `/bin/sh -c`:

- `internal/resources/snapshot_job.go:36`
- `internal/resources/snapshot_job.go:38`
- `internal/resources/snapshot_job.go:73`

Impact: a user allowed to create `HermesSelfConfig` with profiles enabled can execute arbitrary shell in the generated Job.

### Shell Injection In Migration S3 Fields

S3 `bucket`, `endpoint`, and `key` have no validation:

- `api/v1/hermesinstance_types.go:903`

They are interpolated directly into a shell script and AWS CLI command:

- `internal/resources/migration_init.go:65`
- `internal/resources/migration_init.go:69`

Impact: a user able to create/update a `HermesInstance` migration from backup can execute arbitrary commands in the init container.

### `HermesInstance` Creation Is Workload Creation

The CRD exposes raw containers, volumes, env, mounts, sidecars, and init containers:

- `api/v1/hermesinstance_types.go:80`

The builder applies security-context overrides and passthrough fields verbatim:

- `internal/resources/statefulset.go:39`
- `internal/resources/statefulset.go:51`
- `internal/resources/statefulset.go:157`
- `internal/resources/statefulset.go:219`
- `internal/resources/statefulset.go:264`

Impact: create/update permission on `HermesInstance` should be treated as a privileged workload-creation permission.

### Operator RBAC Is Broad

The Helm ClusterRole grants cluster-wide create/update/delete over secrets, workloads, services, ingresses, roles, and rolebindings:

- `charts/hermes-operator/templates/clusterrole.yaml:8`

`watchNamespaces` is exposed but not wired:

- `charts/hermes-operator/values.yaml:7`
- `charts/hermes-operator/templates/deployment.yaml:28`

Impact: installs watch all namespaces and carry broad cluster permissions even when values imply namespace scoping.

### NetworkPolicy Defaults Are Broad

Generated NetworkPolicy allows DNS to any peer:

- `internal/resources/networkpolicy.go:104`

It also allows TCP/443 to any destination:

- `internal/resources/networkpolicy.go:115`

Gateway-specific rules intentionally omit `To`, also allowing all 443 destinations:

- `internal/resources/networkpolicy.go:146`

Ingress allows any pod in the same namespace to exposed service ports:

- `internal/resources/networkpolicy.go:65`

Impact: this is much wider than “default deny plus per-gateway allow” suggests.

### Network Exposure Defaults Need Stronger Guardrails

The CRD permits `NodePort` and `LoadBalancer`:

- `api/v1/hermesinstance_types.go:436`

Service annotations are copied verbatim:

- `api/v1/hermesinstance_types.go:452`

Ingress annotations are merged with user-provided values:

- `internal/resources/ingress.go:42`

Agent metrics default to insecure/plaintext:

- `api/v1/hermesinstance_types.go:566`

The Service exposes metrics by default:

- `internal/resources/service.go:76`

Impact: users can expose workloads and insecure metrics without strong admission guardrails.

### Supply Chain Gaps

The agent image defaults to mutable `latest`:

- `api/v1/hermesinstance_types.go:167`
- `internal/resources/statefulset.go:282`

Docker bases and GitHub Actions are tag-pinned rather than digest/SHA pinned:

- `Dockerfile:4`
- `Dockerfile:20`
- `images/hermes-agent/Dockerfile:25`
- `images/hermes-agent/Dockerfile:27`
- `.github/workflows/ci.yaml:11`

Release tooling uses `version: latest`:

- `.github/workflows/release.yaml:60`
- `.github/workflows/release.yaml:158`

Impact: reproducibility and supply-chain integrity are weaker than the README suggests.

## Code Quality And Architecture Findings

### Good Patterns Worth Preserving

- Clear package split:
  - `api/v1`
  - `internal/controller`
  - `internal/resources`
  - `internal/webhook`
- Pure resource builders in `internal/resources`.
- Deterministic resource naming.
- Shared label helpers.
- Explicit Kubernetes defaults to avoid reconcile drift.
- Foreign label/annotation preservation.
- SelfConfig SSA field-manager model with narrow partial objects.

### Main Quality Risk: Ownership Drift

The largest maintainability issue is not the layout. It is that multiple components own or mutate the same resources without one clear desired-state source.

Examples:

- Auto-update mutates the StatefulSet image out-of-band.
- Main reconciler later overwrites the StatefulSet spec.
- Subcontroller errors are swallowed, so status and reconciliation state diverge.

### Public API Is Ahead Of Implementation

Fields exposed as stable API are not implemented or only partly implemented:

- `spec.storage.persistence.enabled`
- `spec.workspace.configMapRef`
- `spec.workspace.bootstrap`
- `spec.profileStore.honcho.persistence.enabled`
- `spec.runtime.extraAptPackages`
- `HermesSelfConfig.addWorkspaceFiles[].contentFrom`
- logging env wiring
- cluster default networkPolicy doc/implementation alignment

Impact: this creates long-term support debt, especially with v1 stability claims.

### Tooling/Reproducibility Gaps

- `make test` regenerates manifests/code and downloads tooling, so there is no simple read-only test target.
- Controller envtest can fail on metrics port conflicts.
- Helm RBAC check was reported by a subagent to fail with a `jq` shape error.
- Local Go was not initially available outside a Nix shell.

## Live Cluster Assessment

The guarded live cluster subagent found:

- `kubectl` available.
- Cilium installed and healthy.
- Hermes CRDs absent:
  - `hermesinstances.hermes.agent`
  - `hermesclusterdefaults.hermes.agent`
  - `hermesselfconfigs.hermes.agent`
- No Hermes operator deployment or webhook configurations found.

The subagent created an ephemeral namespace and applied a Cilium egress-deny policy before any further checks:

- `endpointSelector: {}`
- `egressDeny: toEntities: [all]`

Because Hermes was not installed, no `HermesInstance` was created and no cluster-wide resources were installed.

Cleanup succeeded.

## Prioritized Remediation

1. Fix command injection.
   - Replace shell-string construction with argv-based execution where possible.
   - Strictly quote or validate every interpolated field.
   - Add allowlist validation for profile IDs, S3 bucket names, endpoints, keys, and snapshot paths.

2. Fix auto-update ownership.
   - Make one component own the desired StatefulSet image.
   - Encode auto-update target/current tag into desired state or patch the CR spec/status in a controlled way.
   - Add tests that force parent and owned-resource reconciles during rollout.

3. Make backup/restore/migration real and tested.
   - Decide whether the format is raw `.tar.zst` or restic repository data.
   - Align docs and implementation.
   - Use tested images containing required tools.
   - Add writable temp mounts.
   - Unskip MinIO backup/restore e2e.

4. Implement or remove unwired API fields before v1 claims.
   - `persistence.enabled`
   - workspace `configMapRef`
   - workspace `bootstrap`
   - SelfConfig `contentFrom`
   - Honcho persistence disablement
   - logging env wiring

5. Fix readiness and subcontroller error propagation.
   - Propagate subcontroller errors and requeue results.
   - Include day-2 conditions in overall `Ready`.
   - Do not mark ready solely from StatefulSet replicas.

6. Tighten security posture.
   - Gate dangerous workload passthrough with webhooks or CEL.
   - Treat `create/update hermesinstances` as privileged.
   - Implement real namespace scoping for `watchNamespaces`.
   - Offer Role/RoleBinding installs where possible.
   - Set `automountServiceAccountToken: false` on generated pods/jobs that do not need API access.

7. Tighten network and metrics defaults.
   - Avoid default all-destination TCP/443 egress.
   - Prefer CNI-specific FQDN policies or explicit selectors/CIDRs.
   - Make agent metrics secure by default or do not expose them by default.

8. Align distribution artifacts.
   - Fix Helm manager metrics/probes/RBAC parity with kustomize.
   - Include or clearly document webhooks in plain manifests.
   - Regenerate and validate OLM bundle metadata.
   - Sync README, examples, chart, values, bundle, and release metadata.

9. Improve release gates.
   - Gate release publishing on conformance.
   - Broaden PR conformance triggers for controller/resource changes.
   - Pin Docker images, GitHub Actions, GoReleaser, Helm, and agent dependencies by digest/SHA or committed lockfiles.

10. Improve developer verification.
    - Add a read-only test target.
    - Avoid default metrics port conflicts in envtest.
    - Make local toolchain setup reproducible through Nix or documented scripts.
