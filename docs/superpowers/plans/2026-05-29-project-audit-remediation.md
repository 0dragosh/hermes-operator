# Hermes Operator Audit Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The intended execution model is parallel GPT-5.5 xhigh subagents where write sets are disjoint, followed by one integration pass for generated artifacts and full verification.

**Goal:** Turn the findings in `docs/project-audit-2026-05-29.md` into a production-readiness remediation sequence that fixes security blockers, correctness gaps, runtime day-2 behavior, distribution drift, and release gates.

**Architecture:** Stabilize verification first, then fix the highest-risk security and ownership defects, then complete runtime and distribution surfaces. Resource builders remain pure and testable. The main `HermesInstance` reconciler is the only owner of generated desired state; subcontrollers report status, errors, and requeue requests instead of mutating owned resources out-of-band. CRDs, Helm CRDs, bundle manifests, and generated RBAC are regenerated once during integration to avoid conflicts between parallel workers.

**Tech Stack:** Go 1.26, controller-runtime, Kubebuilder/controller-gen, envtest, Kubernetes, Cilium for guarded live smoke tests, Helm, kustomize, OLM bundle metadata, GitHub Actions, Docker/buildx, Nix shells for reproducible local tooling.

---

## Source Audit

This plan implements the remediation from:

- `docs/project-audit-2026-05-29.md`

Primary conclusions from the audit:

- The project is not production ready.
- Command injection and admission guardrails are the first blockers.
- Auto-update currently conflicts with normal StatefulSet reconciliation.
- Backup/restore/migration should use the documented raw `.tar.zst` S3 object format, not restic repository state.
- Public API fields must either work, be rejected, or be documented as unsupported before v1 claims.
- Plain manifests, Helm, OLM, examples, and release gates need a distribution hardening pass.

## Parallel Execution Model

Use parallel GPT-5.5 xhigh subagents for independent phases, but keep these ownership rules:

- `internal/controller/hermesinstance_controller.go` has one owner during integration.
- Generated files are not edited by feature subagents. The integration agent runs generation once.
- API marker changes and webhook validation changes can be done together by one API/admission worker.
- Runtime image and backup builder work can proceed in parallel after the raw `.tar.zst` format decision is accepted.
- Distribution work can proceed in parallel with runtime work after Phase 0 verification foundations are in place.

Recommended subagent split:

1. **Verification Foundation Agent**
   - Owns `Makefile`, `internal/controller/suite_test.go`, RBAC scripts, CI read-only test wiring, development docs.
2. **Security Admission Agent**
   - Owns `api/v1/*types.go`, `internal/webhook/*`, security validation tests, conformance negative tests.
3. **Workload Builder Agent**
   - Owns `internal/resources/snapshot_job.go`, `migration_init.go`, `statefulset.go`, `honcho.go`, `backup_job.go`, `backup_cronjob.go`, `restore_init.go`, and matching tests.
4. **Controller Integrator Agent**
   - Owns `internal/controller/hermesinstance_controller.go`, day-2 result aggregation, status readiness, workspace resolution, and controller envtest additions.
5. **Runtime Backup Agent**
   - Owns `images/hermes-agent/*`, agent image workflows, backup/restore/migration docs, MinIO e2e.
6. **Distribution Agent**
   - Owns Helm, kustomize, OLM bundle metadata, release workflows, examples, README/API docs.
7. **Integration Agent**
   - Runs `make generate manifests sync-chart-crds`, bundle sync/update, full test suite, and guarded live cluster smoke.

## Phase 0: Verification Foundation

**Purpose:** Make local and CI verification reliable before changing behavior.

**Parallelism:** One focused subagent. This phase should land before other workers rely on controller envtest or RBAC checks.

**Files:**

- Modify: `Makefile`
- Modify: `internal/controller/suite_test.go`
- Modify: `hack/check-helm-rbac.sh`
- Modify: `hack/sync-bundle-rbac.sh`
- Modify: `.github/workflows/ci.yaml`
- Modify: `.github/workflows/helm-rbac.yaml`
- Modify: `CONTRIBUTING.md`
- Create: `docs/development.md`
- Optional create: `flake.nix`
- Optional create: `flake.lock`

- [ ] **Step 1: Disable envtest manager metrics binding**

  In `internal/controller/suite_test.go`, import:

  ```go
  metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
  ```

  Change manager creation to:

  ```go
  k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
      Scheme:  scheme.Scheme,
      Metrics: metricsserver.Options{BindAddress: "0"},
  })
  ```

  This removes the audited `:8080` bind conflict.

- [ ] **Step 2: Add pinned `yq` tooling**

  In `Makefile`, add:

  ```make
  YQ ?= $(LOCALBIN)/yq
  YQ_VERSION ?= v4.45.1

  .PHONY: yq
  yq: $(YQ) ## Download mikefarah/yq locally if necessary.
  $(YQ): $(LOCALBIN)
      $(call go-install-tool,$(YQ),github.com/mikefarah/yq/v4,$(YQ_VERSION))
  ```

- [ ] **Step 3: Make RBAC scripts use the pinned `yq`**

  In `hack/check-helm-rbac.sh`, set `YQ="${YQ:-./bin/yq}"`, render the exact manager `ClusterRole`, and normalize rule arrays with yq v4 expressions that sort by `apiGroups`, `resources`, and `verbs`.

  In `hack/sync-bundle-rbac.sh`, use the same `YQ="${YQ:-./bin/yq}"` convention and fail with a clear message when `yq --version` is not mikefarah v4.

- [ ] **Step 4: Add read-only test targets**

  Add a non-mutating test target to `Makefile`:

  ```make
  .PHONY: test-readonly
  test-readonly: ## Run read-only unit tests without generating files.
      CGO_ENABLED=0 go test ./api/... ./internal/resources/... ./internal/oci/... ./internal/webhook/... -count=1

  .PHONY: test-controller
  test-controller: envtest ## Run controller envtest with downloaded assets.
      KUBEBUILDER_ASSETS="$$( $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path )" CGO_ENABLED=0 go test ./internal/controller/... -count=1 -ginkgo.v
  ```

  Keep `make test` as the mutating full developer target.

- [ ] **Step 5: Update CI test flow**

  In `.github/workflows/ci.yaml`, use `make test-readonly` for fast read-only tests and keep generation checks in a separate job that runs `make manifests generate` followed by a dirty-tree check.

  In `.github/workflows/helm-rbac.yaml`, install or run `make yq` before RBAC scripts.

- [ ] **Step 6: Document local development**

  Create `docs/development.md` with:

  - Go version: `1.26.x`
  - Nix shell example:

    ```bash
    nix shell nixpkgs#go_1_26 nixpkgs#kubectl nixpkgs#kubernetes-helm nixpkgs#kind nixpkgs#jq nixpkgs#yq-go
    ```

  - Tool versions from `Makefile`.
  - Difference between `make test-readonly`, `make test-controller`, and `make test`.
  - Envtest asset setup.

- [ ] **Step 7: Verify Phase 0**

  Run:

  ```bash
  make yq
  bash hack/check-helm-rbac.sh
  bash hack/sync-bundle-rbac.sh --check
  make test-readonly
  make test-controller
  ```

  If local Go is missing, run the same commands inside:

  ```bash
  nix shell nixpkgs#go_1_26 nixpkgs#kubectl nixpkgs#kubernetes-helm nixpkgs#jq --command bash
  ```

## Phase 1: Command Injection Blockers

**Purpose:** Remove high-severity command injection paths before expanding tests or release surfaces.

**Parallelism:** Two subagents can run in parallel:

- SelfConfig snapshot safety worker.
- Migration S3 safety worker.

Generated CRDs wait for the integration phase.

### Task 1.1: SelfConfig Snapshot Safety

**Files:**

- Modify: `api/v1/hermesselfconfig_types.go`
- Modify: `internal/webhook/webhook_hermesselfconfig.go`
- Modify: `internal/resources/snapshot_job.go`
- Test: `api/v1/hermesselfconfig_types_test.go`
- Test: `internal/webhook/webhook_hermesselfconfig_validate_test.go`
- Test: `internal/resources/snapshot_job_test.go`
- Test: `test/conformance/negative_test.go`

- [ ] **Step 1: Tighten `profileID` schema**

  In `api/v1/hermesselfconfig_types.go`, change `ProfileID` markers to:

  ```go
  // +kubebuilder:validation:MinLength=1
  // +kubebuilder:validation:MaxLength=253
  // +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
  ProfileID string `json:"profileID"`
  ```

- [ ] **Step 2: Add webhook profile validation**

  In `internal/webhook/webhook_hermesselfconfig.go`, reject `addProfileSnapshot.profileID` values that do not match the same pattern. Use a package-level regexp so tests can exercise the exact rule.

- [ ] **Step 3: Remove dynamic shell interpolation**

  In `internal/resources/snapshot_job.go`, build a static shell script and pass values through environment variables:

  ```go
  Command: []string{"/bin/sh", "-c"},
  Args: []string{`set -eu
  mkdir -p "$(dirname "$SNAPSHOT_PATH")"
  printf '%s' "$SNAPSHOT_DATA" > "$SNAPSHOT_PATH"
  `},
  Env: []corev1.EnvVar{
      {Name: "SNAPSHOT_PATH", Value: relPath},
      {Name: "SNAPSHOT_DATA", Value: data},
  },
  ```

  `relPath` must use the validated profile ID only:

  ```go
  relPath := fmt.Sprintf("/data/snapshots/%s/%s.json", profileID, rfc3339)
  ```

- [ ] **Step 4: Harden snapshot Job pod spec**

  Add:

  ```go
  AutomountServiceAccountToken: Ptr(false),
  ```

  to the snapshot Job pod spec.

- [ ] **Step 5: Add tests**

  Add tests that assert:

  - `profileID: "prod; touch /tmp/pwned"` is rejected.
  - `profileID: "../prod"` is rejected.
  - `profileID: "prod-user-1"` is accepted.
  - Snapshot Job command text does not contain profile ID or data payload.
  - Snapshot Job env contains `SNAPSHOT_PATH` and `SNAPSHOT_DATA`.
  - Snapshot Job sets `automountServiceAccountToken=false`.

- [ ] **Step 6: Verify Task 1.1**

  Run:

  ```bash
  CGO_ENABLED=0 go test ./api/... ./internal/resources/... ./internal/webhook/... -run 'SelfConfig|Snapshot|HermesSelfConfig' -count=1
  ```

### Task 1.2: Migration S3 Shell Safety

**Files:**

- Modify: `api/v1/hermesinstance_types.go`
- Modify: `internal/webhook/webhook_hermesinstance_validate.go`
- Modify: `internal/resources/migration_init.go`
- Test: `internal/webhook/webhook_hermesinstance_validate_test.go`
- Test: `internal/resources/migration_init_test.go`
- Test: `test/conformance/negative_test.go`

- [ ] **Step 1: Add S3 field schema markers**

  In `api/v1/hermesinstance_types.go`, add length and pattern markers for:

  - `BackupS3Spec.Bucket`
  - `BackupS3Spec.Endpoint`
  - `BackupS3Spec.PathPrefix`
  - `MigrationBackupS3.Bucket`
  - `MigrationBackupS3.Endpoint`
  - `MigrationBackupS3.Key`

  Use rules compatible with raw object keys:

  ```go
  // +kubebuilder:validation:MinLength=3
  // +kubebuilder:validation:MaxLength=63
  // +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9.-]*[a-z0-9]$`
  Bucket string `json:"bucket"`
  ```

  For keys, reject traversal and shell metacharacter control characters via webhook even if CRD regex remains permissive enough for normal prefixes.

- [ ] **Step 2: Validate S3 fields in webhook**

  In `internal/webhook/webhook_hermesinstance_validate.go`, add validation helpers:

  ```go
  func validateS3Bucket(path *field.Path, bucket string) field.ErrorList
  func validateS3Endpoint(path *field.Path, endpoint string) field.ErrorList
  func validateS3ObjectKey(path *field.Path, key string) field.ErrorList
  ```

  Reject:

  - Empty bucket when a backup or migration S3 source is configured.
  - `..` path segments in keys and prefixes.
  - Control characters.
  - Endpoint values containing shell metacharacters, whitespace, or path traversal.

- [ ] **Step 3: Stop interpolating S3 fields into migration shell text**

  In `internal/resources/migration_init.go`, replace the S3 `fmt.Sprintf` script with a static script:

  ```go
  const migrationFromS3Script = `set -euo pipefail
  mkdir -p /mnt/openclaw
  echo "Downloading OpenClaw snapshot ${OPENCLAW_S3_BUCKET}/${OPENCLAW_S3_KEY}" >&2
  aws --endpoint-url "$OPENCLAW_S3_ENDPOINT_URL" s3 cp "s3://${OPENCLAW_S3_BUCKET}/${OPENCLAW_S3_KEY}" - --no-progress \
    | zstd -d \
    | tar -xf - -C /mnt/openclaw
  echo "Running hermes-agent importer against extracted snapshot" >&2
  hermes-agent migrate from-openclaw --source /mnt/openclaw --dest /home/hermes/.hermes
  `
  ```

  Add env vars:

  ```go
  {Name: "OPENCLAW_S3_BUCKET", Value: s3.Bucket}
  {Name: "OPENCLAW_S3_KEY", Value: s3.Key}
  {Name: "OPENCLAW_S3_ENDPOINT_URL", Value: normalizedEndpointURL(s3.Endpoint)}
  ```

- [ ] **Step 4: Add writable `/tmp` and token hardening**

  In migration init containers, add:

  ```go
  VolumeMounts: append(mounts, corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"})
  ```

  The owning StatefulSet builder must include the `tmp` volume, which already exists for the main pod.

- [ ] **Step 5: Add tests**

  Add tests that assert:

  - Shell args do not contain the literal S3 bucket, key, or endpoint.
  - S3 values appear only in env vars.
  - `../`, `;`, newline, and whitespace injection attempts are rejected by webhook.
  - Migration init has a `/tmp` mount.

- [ ] **Step 6: Verify Task 1.2**

  Run:

  ```bash
  CGO_ENABLED=0 go test ./internal/resources/... ./internal/webhook/... -run 'Migration|S3|Validate' -count=1
  ```

## Phase 2: Admission Guardrails And API Contract

**Purpose:** Ensure dangerous public API inputs are explicitly admitted or denied instead of silently creating unsafe workloads.

**Parallelism:** One admission/API subagent. This phase touches shared API and webhook files and should not run concurrently with other API marker edits.

**Files:**

- Modify: `api/v1/hermesinstance_types.go`
- Modify: `api/v1/hermesselfconfig_types.go`
- Modify: `internal/webhook/webhook_hermesinstance_validate.go`
- Create: `internal/webhook/security_validation.go`
- Test: `internal/webhook/webhook_hermesinstance_validate_test.go`
- Test: `internal/webhook/webhook_hermesselfconfig_validate_test.go`
- Test: `api/v1/security_validation_markers_test.go`
- Test: `test/conformance/negative_test.go`

- [ ] **Step 1: Add SelfConfig xor validation**

  Enforce:

  - `addEnvVars[].value` xor `addEnvVars[].valueFrom`.
  - `valueFrom.secretKeyRef` xor `valueFrom.configMapKeyRef`.
  - `addWorkspaceFiles[].content` xor `addWorkspaceFiles[].contentFrom`.

  Put webhook checks in `internal/webhook/webhook_hermesselfconfig.go` and CRD markers in `api/v1/hermesselfconfig_types.go`.

- [ ] **Step 2: Mark `runtime.extraAptPackages` unsupported**

  Keep the field for API compatibility, but change the Go comment to state that non-empty values are rejected because init-container package installs do not affect the main container filesystem.

  In `internal/webhook/webhook_hermesinstance_validate.go`, reject:

  ```go
  len(inst.Spec.Runtime.ExtraAptPackages) > 0
  ```

  with an error that says to build a custom agent image.

- [ ] **Step 3: Reject unsafe workload passthrough**

  In `internal/webhook/security_validation.go`, implement helpers that inspect:

  - `spec.initContainers`
  - `spec.sidecars`
  - `spec.extraVolumes`
  - `spec.extraVolumeMounts`
  - `spec.security.podSecurityContext`
  - `spec.security.containerSecurityContext`

  Reject:

  - `securityContext.privileged=true`
  - `allowPrivilegeEscalation=true`
  - `runAsUser=0`
  - added Linux capabilities
  - `hostPath` volumes
  - projected service account token volumes
  - mounts at `/`, `/proc`, `/sys`, `/var/run/docker.sock`
  - `hostNetwork`, `hostPID`, or `hostIPC` if those fields become exposed later

- [ ] **Step 4: Reject unsafe network exposure by default**

  In the webhook, reject:

  - `spec.networking.service.type=NodePort`
  - `spec.networking.service.type=LoadBalancer`
  - ingress enabled without TLS
  - annotations that disable TLS, auth, or request public load balancers
  - metrics enabled with `secure=false`

  If maintainers want these escape hatches later, add a separate admin-owned policy API. Do not add the escape hatch in this remediation phase.

- [ ] **Step 5: Add marker canary tests**

  Add API tests that read generated schema snippets or use type comments to assert CEL markers exist for:

  - migration exactly-one source
  - SelfConfig xor fields
  - S3/profile safety markers

  Generated CRD verification happens in Phase 9.

- [ ] **Step 6: Verify Phase 2**

  Run:

  ```bash
  CGO_ENABLED=0 go test ./api/... ./internal/webhook/... -count=1
  ```

## Phase 3: Pure Resource Builder Correctness

**Purpose:** Fix features that are advertised in the API but not represented correctly in desired Kubernetes objects.

**Parallelism:** Four subagents can run in parallel if they keep write sets disjoint:

- Persistence and metrics worker.
- Honcho and runtime worker.
- Workspace worker.
- Network policy worker.

### Task 3.1: Shared Builder Helpers

**Files:**

- Modify: `internal/resources/common.go`
- Test: `internal/resources/common_test.go`

- [ ] **Step 1: Add effective-value helpers**

  Add helpers:

  ```go
  func PersistenceEnabled(inst *hermesv1.HermesInstance) bool {
      return BoolValueOrDefault(inst.Spec.Storage.Persistence.Enabled, true)
  }

  func MetricsEnabled(inst *hermesv1.HermesInstance) bool {
      return BoolValueOrDefault(inst.Spec.Observability.Metrics.Enabled, true)
  }

  func EffectiveMetricsPort(inst *hermesv1.HermesInstance) int32 {
      if inst.Spec.Observability.Metrics.Port != 0 {
          return inst.Spec.Observability.Metrics.Port
      }
      return DefaultMetricsPort
  }

  func EffectiveAgentTag(inst *hermesv1.HermesInstance) string {
      if inst.Spec.AutoUpdate.Enabled {
          if inst.Status.AutoUpdate.TargetTag != "" {
              return inst.Status.AutoUpdate.TargetTag
          }
          if inst.Status.AutoUpdate.CurrentTag != "" {
              return inst.Status.AutoUpdate.CurrentTag
          }
      }
      return inst.Spec.Image.Tag
  }
  ```

  If `DefaultMetricsPort` already exists elsewhere, move or reuse it without duplicate constants.

- [ ] **Step 2: Add tests**

  Cover default behavior, disabled persistence, metrics port default, target tag precedence, and current tag fallback.

### Task 3.2: `storage.persistence.enabled=false`

**Files:**

- Modify: `internal/resources/statefulset.go`
- Test: `internal/resources/statefulset_test.go`
- Modify later by controller integrator: `internal/controller/hermesinstance_controller.go`

- [ ] **Step 1: Use `emptyDir` when persistence is disabled**

  In `BuildStatefulSet`, change the `data` volume source:

  ```go
  dataVolume := corev1.Volume{
      Name: "data",
      VolumeSource: corev1.VolumeSource{
          PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: PVCName(inst)},
      },
  }
  if !PersistenceEnabled(inst) {
      dataVolume.VolumeSource = corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
  }
  ```

- [ ] **Step 2: Add tests**

  Assert:

  - Default uses PVC.
  - `enabled=false` uses `emptyDir`.
  - Mount path remains `/home/hermes/.hermes`.

### Task 3.3: Metrics Scraping Alignment

**Files:**

- Modify: `internal/resources/statefulset.go`
- Modify: `internal/resources/service.go`
- Modify: `internal/resources/servicemonitor.go`
- Test: `internal/resources/statefulset_test.go`
- Test: `internal/resources/service_test.go`
- Test: `internal/resources/servicemonitor_test.go`

- [ ] **Step 1: Add metrics container port when metrics are enabled**

  Append to the main container ports:

  ```go
  if MetricsEnabled(inst) {
      c.Ports = append(c.Ports, corev1.ContainerPort{
          Name:          MetricsPortName,
          ContainerPort: EffectiveMetricsPort(inst),
          Protocol:      corev1.ProtocolTCP,
      })
  }
  ```

- [ ] **Step 2: Keep service target port named `metrics`**

  Keep `BuildService` using `TargetPort: intstr.FromString(MetricsPortName)`, and add tests that the StatefulSet exposes the same named port.

- [ ] **Step 3: Require secure metrics in ServiceMonitor**

  Change ServiceMonitor builder to emit HTTPS/auth settings when `metrics.secure=true`, and rely on Phase 2 webhook to reject insecure metrics.

- [ ] **Step 4: Verify Task 3.3**

  Run:

  ```bash
  CGO_ENABLED=0 go test ./internal/resources/... -run 'Metrics|ServiceMonitor|StatefulSet' -count=1
  ```

### Task 3.4: Honcho Persistence Disabled

**Files:**

- Modify: `internal/resources/honcho.go`
- Test: `internal/resources/honcho_test.go`
- Add focused controller test: `internal/controller/honcho_persistence_test.go`

- [ ] **Step 1: Use `emptyDir` for Honcho data when disabled**

  In `BuildHonchoDeployment`, set the `honcho-data` volume to PVC only when:

  ```go
  BoolValueOrDefault(inst.Spec.ProfileStore.Honcho.Persistence.Enabled, true)
  ```

  Otherwise use:

  ```go
  corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
  ```

- [ ] **Step 2: Add tests**

  Assert:

  - Default Honcho deployment mounts PVC.
  - `profileStore.honcho.persistence.enabled=false` mounts `emptyDir`.
  - Controller skips Honcho PVC creation when persistence is disabled.

### Task 3.5: Remove Broken `extraAptPackages` Runtime Path

**Files:**

- Modify: `internal/resources/runtime_init.go`
- Test: `internal/resources/runtime_init_test.go`
- Modify: `docs/api-reference.md`
- Modify: examples that mention `extraApt` or `extraAptPackages`

- [ ] **Step 1: Remove `init-apt` emission**

  In `BuildRuntimeInitContainers`, remove the branch that appends `buildAptInit`.

- [ ] **Step 2: Keep `/tmp` writable for runtime init containers**

  Add `{Name: "tmp", MountPath: "/tmp"}` to `init-uv` and `init-pip` volume mounts.

- [ ] **Step 3: Update tests**

  Assert:

  - No `init-apt` is generated even when `ExtraAptPackages` is non-empty.
  - Webhook rejects non-empty `ExtraAptPackages` from Phase 2.
  - `init-uv` and `init-pip` have `/tmp`.

### Task 3.6: Workspace ConfigMap Ref And Bootstrap Builder Shape

**Files:**

- Modify: `internal/resources/workspace_configmap.go`
- Test: `internal/resources/workspace_configmap_test.go`
- Modify later by controller integrator: `internal/controller/hermesinstance_controller.go`
- Modify: `internal/resources/runtime_init.go`
- Test: `internal/resources/runtime_init_test.go`

- [ ] **Step 1: Let builder accept base data**

  Change the workspace builder signature to:

  ```go
  func BuildWorkspaceConfigMap(inst *hermesv1.HermesInstance, base map[string]string) *corev1.ConfigMap
  ```

  Merge order:

  1. Copy `base`.
  2. Overlay encoded `spec.workspace.initialFiles`.
  3. Set `__hermes_initial_dirs__` from `initialDirs`.

- [ ] **Step 2: Add bootstrap init container support**

  If `workspace.bootstrap.enabled=true`, add an init container that:

  - Reads `/home/hermes/.hermes-workspace-seed`.
  - Creates paths from `__hermes_initial_dirs__`.
  - Copies seed files once using a sentinel file under the data PVC.
  - Runs the supported `hermes-agent` bootstrap/onboarding command only after seed copy.

  Keep the shell script static; pass paths through env vars.

- [ ] **Step 3: Add tests**

  Assert:

  - Referenced ConfigMap data is preserved.
  - `initialFiles` win on key conflict.
  - `initialDirs` key is deterministic.
  - Bootstrap init is absent by default and present when enabled.

### Task 3.7: NetworkPolicy Defaults

**Files:**

- Modify: `internal/resources/networkpolicy.go`
- Test: `internal/resources/networkpolicy_test.go`
- Modify: `docs/runbook-platform-gateways.md`
- Modify: `docs/api-reference.md`

- [ ] **Step 1: Remove broad TCP/443 egress**

  Delete the default egress rule with empty `To` and TCP/443.

- [ ] **Step 2: Remove gateway egress rules with empty `To`**

  Change `ExtraEgressRules` so it emits no broad all-destination 443 rule. Users must configure `allowedEgressCIDRs` or `additionalEgress` until CNI-specific FQDN policies are added.

- [ ] **Step 3: Avoid broad same-namespace ingress by default**

  Either require explicit `allowedIngressNamespaces` or add an explicit `allowSameNamespaceIngress` field defaulting false. If adding the field, put it in `NetworkPolicySpec` and cover it in webhook/CRD generation.

- [ ] **Step 4: Keep DNS behavior explicit**

  Retain `allowDNS=true`, but document that Kubernetes NetworkPolicy cannot portably target CoreDNS without cluster-specific selectors. For stricter installs, users should set `allowDNS=false` and supply `additionalEgress`.

- [ ] **Step 5: Add tests**

  Assert there is no empty-peer TCP/443 egress rule by default and gateway enablement does not add one.

## Phase 4: Controller Ownership, Status, And Day-2 Propagation

**Purpose:** Make the main reconciler the single desired-state owner, make status reflect actual subsystem health, and stop swallowing day-2 errors/requeues.

**Parallelism:** Single controller integrator. Do not edit `internal/controller/hermesinstance_controller.go` from other workers during this phase.

**Files:**

- Modify: `internal/controller/hermesinstance_controller.go`
- Test: `internal/controller/hermesinstance_controller_test.go`
- Create: `internal/controller/status_test.go`
- Create: `internal/controller/result_test.go`
- Modify: `internal/controller/backup.go`
- Modify: `internal/controller/restore.go`
- Modify: `internal/controller/migration.go`
- Test: `internal/controller/backup_test.go`
- Test: `internal/controller/restore_test.go`
- Test: `internal/controller/migration_test.go`

- [ ] **Step 1: Skip PVC reconciliation when persistence is disabled**

  In `reconcilePVC`, return nil without creating a PVC when `resources.PersistenceEnabled(inst)` is false. Set `StorageReady=True/Disabled` or omit it consistently, then document the chosen condition behavior in `docs/conditions.md`.

- [ ] **Step 2: Resolve `workspace.configMapRef` in the controller**

  In `reconcileWorkspaceConfigMap`, if `inst.Spec.Workspace.ConfigMapRef != nil`, load the referenced ConfigMap in the instance namespace and pass `user.Data` into `BuildWorkspaceConfigMap(inst, user.Data)`.

  Missing ConfigMap should return an error and set `ConfigReady=False`.

- [ ] **Step 3: Aggregate subcontroller results**

  Add helper:

  ```go
  func mergeResults(current, next ctrl.Result) ctrl.Result {
      if next.Requeue {
          current.Requeue = true
      }
      if next.RequeueAfter > 0 && (current.RequeueAfter == 0 || next.RequeueAfter < current.RequeueAfter) {
          current.RequeueAfter = next.RequeueAfter
      }
      return current
  }
  ```

  Use it for backup, migration, restore, and auto-update. Return errors instead of logging and continuing.

- [ ] **Step 4: Make day-2 subcontrollers return actionable results**

  Change subcontroller signatures only where needed so restore/migration can return:

  - Waiting for pod/init state: `RequeueAfter`.
  - Terminal failure: error after condition is set.
  - Completed latch: no requeue.

  Backup with persistence disabled should set `BackupReady=False/PersistenceDisabled` and not create CronJobs.

- [ ] **Step 5: Compute `Ready` from active subsystem conditions**

  Add a helper that determines required condition types from enabled features:

  - Always: Config, Service, StatefulSet readiness.
  - Storage only when persistence enabled.
  - NetworkPolicy/RBAC when enabled.
  - PDB/HPA/Ingress/ServiceMonitor/PrometheusRule when enabled.
  - ProfileStore when Honcho enabled.
  - Backup/Restore/Migration/AutoUpdate when configured or in progress.

  `Ready=True` only when all required conditions are `True` and StatefulSet replicas are ready.

- [ ] **Step 6: Add controller tests**

  Add envtest coverage for:

  - Persistence disabled does not create PVC and StatefulSet uses `emptyDir`.
  - Missing workspace ConfigMap ref sets a false condition and does not report Ready.
  - Day-2 subcontroller error is returned by reconcile.
  - Shortest `RequeueAfter` wins across subcontrollers.
  - Ready remains false when Honcho/backup/migration/restore is unhealthy.

- [ ] **Step 7: Verify Phase 4**

  Run:

  ```bash
  make test-controller
  ```

## Phase 5: Auto-Update Ownership

**Purpose:** Stop auto-update from being overwritten by normal reconciliation.

**Parallelism:** One auto-update worker. Can run after Phase 3 helper functions land and before or alongside Phase 4 if the controller integrator owns final merge.

**Files:**

- Modify: `internal/resources/statefulset.go`
- Test: `internal/resources/statefulset_test.go`
- Modify: `internal/controller/autoupdate.go`
- Test: `internal/controller/autoupdate_test.go`
- Modify as needed: `internal/oci/fake.go`

- [ ] **Step 1: Make StatefulSet image read auto-update status**

  Update `imageRef(inst)` so the tag comes from `resources.EffectiveAgentTag(inst)`. Keep repository resolution unchanged.

- [ ] **Step 2: Stop patching StatefulSet in auto-update**

  In `AutoUpdateReconciler.startRollout`, remove direct StatefulSet image patching. The subcontroller should set:

  - `status.autoUpdate.targetTag`
  - `status.autoUpdate.rolloutDeadline`
  - `status.autoUpdate.probeFailures`
  - auto-update condition

  The next main reconcile updates the StatefulSet.

- [ ] **Step 3: Make confirm and rollback update status before desired state**

  In `confirmRollout`, set `currentTag=targetTag`, then clear `targetTag`.

  In `rollback`, set `currentTag` to the previous known-good tag, clear `targetTag`, set `lastFailedTag`, and let main reconcile update the StatefulSet image.

- [ ] **Step 4: Add tests**

  Add tests that simulate:

  - `targetTag` set: builder uses target tag.
  - `currentTag` set: builder uses current tag.
  - Owned StatefulSet reconcile during rollout does not revert image to `spec.image.tag`.
  - Rollback status causes the builder to use previous tag.

- [ ] **Step 5: Verify Phase 5**

  Run:

  ```bash
  CGO_ENABLED=0 go test ./internal/resources/... ./internal/controller/... -run 'AutoUpdate|StatefulSet' -count=1
  ```

## Phase 6: Raw Backup, Restore, Migration Runtime

**Purpose:** Make backup/restore/migration match docs and actually run in hardened pods.

**Parallelism:** Three subagents can run in parallel after the raw `.tar.zst` decision is accepted:

- Agent image/toolchain worker.
- Backup builder worker.
- Restore/migration builder worker.

### Task 6.1: Agent Image Toolchain

**Files:**

- Create: `images/hermes-agent/uv.lock`
- Modify: `images/hermes-agent/Dockerfile`
- Modify: `images/hermes-agent/README.md`
- Modify: `Makefile`
- Modify: `.github/workflows/agent-image.yaml`
- Modify: `.github/workflows/agent-image-smoke.yaml`
- Modify: `.github/workflows/e2e.yaml`

- [ ] **Step 1: Generate and commit `uv.lock`**

  Run:

  ```bash
  make agent-image-relock HERMES_VERSION=v0.13.0
  ```

  If Docker or network access is unavailable locally, run inside Nix with Docker available, or update the workflow so CI is the source of truth and locally document the command.

- [ ] **Step 2: Install required tools in the agent image**

  In `images/hermes-agent/Dockerfile`, ensure the runtime image includes:

  - `awscli`
  - `zstd`
  - `tar`
  - `coreutils` or equivalent `mktemp`
  - existing `ffmpeg`, `ripgrep`, `git`, `ssh`, certs, and `tini`

- [ ] **Step 3: Make smoke fail when lockfile is absent**

  Remove workflow logic that skips when `images/hermes-agent/uv.lock` is absent.

- [ ] **Step 4: Smoke required binaries**

  In `.github/workflows/agent-image-smoke.yaml`, verify:

  ```bash
  hermes-agent --help
  aws --version
  zstd --version
  tar --version
  mktemp --version || mktemp -t hermes.XXXXXX
  ```

- [ ] **Step 5: Verify Task 6.1**

  Run:

  ```bash
  make agent-image-build AGENT_IMAGE=hermes-agent HERMES_VERSION=v0.13.0
  make agent-image-smoke AGENT_IMAGE=hermes-agent HERMES_VERSION=v0.13.0
  ```

### Task 6.2: Raw Backup Jobs

**Files:**

- Modify: `internal/resources/backup_job.go`
- Modify: `internal/resources/backup_cronjob.go`
- Test: `internal/resources/backup_job_test.go`
- Test: `internal/resources/backup_cronjob_test.go`
- Modify: `internal/controller/s3.go`
- Test: `internal/controller/backup_test.go`

- [ ] **Step 1: Default backup image to agent image**

  Replace `ResticImage` default behavior with the instance agent image while still honoring `spec.backup.image`.

- [ ] **Step 2: Use explicit AWS env vars**

  Map Secret keys:

  - `S3_ACCESS_KEY_ID` to `AWS_ACCESS_KEY_ID`
  - `S3_SECRET_ACCESS_KEY` to `AWS_SECRET_ACCESS_KEY`

  Use `ValueFrom.SecretKeyRef`; do not use broad `EnvFrom`.

- [ ] **Step 3: Use static raw backup script**

  One-shot and scheduled backup scripts should:

  - Create a temp work dir under `/tmp`.
  - Write `meta.json`.
  - Tar `/home/hermes/.hermes`.
  - Compress with zstd.
  - Upload to `s3://$S3_BUCKET/$SNAPSHOT_KEY` with `aws s3 cp -`.

  The script must read bucket, key, endpoint URL, and region from env vars.

- [ ] **Step 4: Add `/tmp` emptyDir and token hardening**

  Backup and prune pods need:

  ```go
  AutomountServiceAccountToken: Ptr(false)
  ```

  and an `emptyDir` volume mounted at `/tmp`.

- [ ] **Step 5: Replace restic prune**

  Prune raw objects under:

  ```text
  <pathPrefix><namespace>/<name>/
  <pathPrefix><namespace>/<name>/failed/
  ```

  Keep newest `historyLimit` and `failedHistoryLimit` `.tar.zst` objects by lexicographic timestamp key.

- [ ] **Step 6: Add tests**

  Assert:

  - No `restic` appears in backup/prune commands.
  - S3 values are env vars, not shell-interpolated.
  - Secret refs are explicit key refs.
  - `/tmp` volume and mount exist.
  - Token automount is false.

### Task 6.3: Raw Restore And Migration

**Files:**

- Modify: `internal/resources/restore_init.go`
- Modify: `internal/resources/migration_init.go`
- Test: `internal/resources/restore_init_test.go`
- Test: `internal/resources/migration_init_test.go`
- Test: `internal/controller/restore_test.go`
- Test: `internal/controller/migration_test.go`

- [ ] **Step 1: Restore from raw S3 object**

  Replace restic dump with:

  ```sh
  aws --endpoint-url "$S3_ENDPOINT_URL" s3 cp "s3://${S3_BUCKET}/${SNAPSHOT_KEY}" - --no-progress \
    | zstd -d \
    | tar -xf - -C "$DEST"
  ```

  Keep the existing empty-destination guard and `HERMES_RESTORE_FORCE`.

- [ ] **Step 2: Use env vars for S3 values**

  Add env vars:

  - `S3_BUCKET`
  - `SNAPSHOT_KEY`
  - `S3_ENDPOINT_URL`
  - `AWS_DEFAULT_REGION`

  Keep credentials as explicit key refs.

- [ ] **Step 3: Add `/tmp` mounts**

  `init-restore` and S3 migration init containers must have `/tmp` mounted from the pod `tmp` emptyDir.

- [ ] **Step 4: Add tests**

  Assert:

  - Restore/migration commands contain no raw bucket/key/endpoint values.
  - No `restic` appears.
  - `/tmp` mount exists.
  - Empty destination guard remains.

### Task 6.4: MinIO E2E

**Files:**

- Modify: `test/e2e/backup_restore_test.go`
- Modify: `test/e2e/minio_test.go`
- Modify: `test/e2e/e2e_suite_test.go`
- Modify: `.github/workflows/e2e.yaml`
- Modify as needed: `Makefile`

- [ ] **Step 1: Remove backup/restore skip**

  Enable the MinIO-backed backup/restore spec.

- [ ] **Step 2: Use locally built images**

  E2E workflow should build and load:

  ```bash
  kind load docker-image hermes-operator:dev --name <kind-cluster>
  kind load docker-image hermes-agent:v0.13.0 --name <kind-cluster>
  ```

- [ ] **Step 3: Assert raw object format**

  The test should confirm a `.tar.zst` object exists under:

  ```text
  e2e/default/e2e-br/
  ```

  It should also assert no restic repository layout prefixes exist:

  - `config/`
  - `data/`
  - `index/`
  - `snapshots/`

- [ ] **Step 4: Assert restore latch**

  Verify:

  ```go
  inst.Status.RestoredFrom == snapshotKey
  ```

## Phase 7: SelfConfig `contentFrom`

**Purpose:** Make the declared `HermesSelfConfig.addWorkspaceFiles[].contentFrom` feature work.

**Parallelism:** One SelfConfig worker. It can run in parallel with backup work if it does not touch generated RBAC until integration.

**Files:**

- Modify: `internal/controller/selfconfig_apply.go`
- Test: `internal/controller/selfconfig_apply_test.go`
- Modify: `internal/controller/hermesselfconfig_controller.go`
- Test: `internal/controller/hermesselfconfig_controller_test.go`
- Modify via generation: `config/rbac/role.yaml`

- [ ] **Step 1: Add Secret read RBAC marker**

  In the SelfConfig controller file, add or confirm RBAC for same-namespace Secret reads:

  ```go
  // +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
  ```

- [ ] **Step 2: Resolve `contentFrom` before building ConfigMap patch**

  When a workspace file has `ContentFrom`, load the referenced same-namespace Secret and read the specified key. Put the resolved bytes into the workspace ConfigMap data value.

- [ ] **Step 3: Deny cleanly on missing Secret/key**

  Missing Secret or key should set SelfConfig phase `Denied`, record a condition, and emit an event. It must not partially apply other requested mutations from the same resource.

- [ ] **Step 4: Add envtest**

  Create a Secret, create a SelfConfig with `contentFrom`, and assert the workspace ConfigMap contains:

  ```text
  resources.EncodeWorkspacePath(path) -> secret value
  ```

## Phase 8: Distribution, Helm, Kustomize, OLM, Docs

**Purpose:** Make shipped artifacts match runtime behavior and validation promises.

**Parallelism:** Distribution work can split into Helm/kustomize, OLM, docs/examples, and release-gates workers after Phase 0. OLM finalization waits for Helm/kustomize shape.

### Task 8.1: Helm And Kustomize Parity

**Files:**

- Modify: `config/default/kustomization.yaml`
- Create: `config/webhook/kustomization.yaml`
- Create: `config/webhook/service.yaml`
- Create: `config/default/manager_webhook_patch.yaml`
- Create: `config/default/webhookcainjection_patch.yaml`
- Create: `config/certmanager/kustomization.yaml`
- Create: `config/certmanager/issuer.yaml`
- Create: `config/certmanager/certificate.yaml`
- Modify: `charts/hermes-operator/values.yaml`
- Modify: `charts/hermes-operator/templates/deployment.yaml`
- Create: `charts/hermes-operator/templates/metrics-service.yaml`
- Create: Helm metrics auth RBAC templates
- Create: `hack/check-distribution-parity.sh`
- Modify: `Makefile`

- [ ] **Step 1: Enable webhooks in kustomize**

  Add webhook and cert-manager resources to `config/default/kustomization.yaml`.

- [ ] **Step 2: Add kustomize webhook Service and manager patch**

  Manager deployment must expose webhook port `9443` and mount certs at:

  ```text
  /tmp/k8s-webhook-server/serving-certs
  ```

- [ ] **Step 3: Add cert-manager resources**

  Add Issuer and Certificate resources and CA injection patches for mutating and validating webhook configurations.

- [ ] **Step 4: Wire Helm manager metrics**

  In Helm deployment:

  - Add `--metrics-bind-address=:8443` when metrics enabled.
  - Add `--metrics-bind-address=0` when metrics disabled.
  - Keep `--metrics-secure`.
  - Add `--leader-elect`.
  - Add probes for `/healthz` and `/readyz` on `:8081`.

- [ ] **Step 5: Add Helm metrics Service and auth RBAC**

  Mirror kustomize metrics auth RBAC:

  - metrics auth ClusterRole
  - metrics auth ClusterRoleBinding
  - metrics reader ClusterRole
  - metrics Service

- [ ] **Step 6: Add parity script**

  `hack/check-distribution-parity.sh` should assert both Helm and kustomize render:

  - WebhookConfiguration
  - Certificate
  - Issuer
  - metrics bind arg
  - livenessProbe
  - readinessProbe
  - metrics Service
  - metrics auth RBAC

- [ ] **Step 7: Verify Task 8.1**

  Run:

  ```bash
  helm template hermes-operator charts/hermes-operator | rg 'WebhookConfiguration|Certificate|Issuer|metrics-bind-address|livenessProbe|readinessProbe'
  kubectl kustomize config/default | rg 'WebhookConfiguration|Certificate|Issuer|metrics-bind-address|livenessProbe|readinessProbe'
  make installer VERSION=v0.1.8
  rg 'WebhookConfiguration|Certificate|Issuer' dist/install.yaml
  bash hack/check-distribution-parity.sh
  ```

### Task 8.2: OLM Bundle Metadata

**Files:**

- Modify: `bundle/manifests/hermes-operator.clusterserviceversion.yaml`
- Modify: `bundle/manifests/hermes.agent_*.yaml`
- Modify: `bundle/metadata/annotations.yaml`
- Modify: `Makefile`
- Create: `hack/update-bundle-metadata.sh`
- Modify: `.github/workflows/operatorhub-submit.yaml`
- Modify: `release-please-config.json`
- Modify: `.github/workflows/release.yaml`

- [ ] **Step 1: Add bundle metadata script**

  Create a script that accepts:

  ```bash
  VERSION=0.1.8
  IMG=ghcr.io/paperclipinc/hermes-operator:v0.1.8
  ```

  and updates:

  - `metadata.name=hermes-operator.v0.1.8`
  - `metadata.annotations.containerImage`
  - `spec.version=0.1.8`
  - related image
  - deployment container image
  - manager args/probes/ports

- [ ] **Step 2: Fix release-please config**

  Remove CSV extra-files that write raw versions into fields requiring templated names or images. Bundle metadata script owns those fields.

- [ ] **Step 3: Add webhook definitions or document limitation**

  Preferred path: add OLM CSV webhook definitions matching Helm/kustomize admission paths.

  If OLM webhooks are deferred, explicitly document that OLM install is not production-supported until webhook definitions land. This should be a temporary state only.

- [ ] **Step 4: Verify Task 8.2**

  Run:

  ```bash
  make bundle VERSION=0.1.8 IMG=ghcr.io/paperclipinc/hermes-operator:v0.1.8
  make bundle-validate
  bash hack/sync-bundle-rbac.sh --check
  rg 'hermes-operator.v0.1.8|ghcr.io/paperclipinc/hermes-operator:v0.1.8|webhookdefinitions' bundle/manifests/hermes-operator.clusterserviceversion.yaml
  ```

### Task 8.3: Examples And Docs Sync

**Files:**

- Modify: `README.md`
- Modify: `docs/api-reference.md`
- Modify: `docs/backup-restore.md`
- Modify: `docs/backup-format.md`
- Modify: `docs/migration.md`
- Modify: `docs/conditions.md`
- Modify: `examples/README.md`
- Modify: all `examples/**/hermesinstance.yaml`
- Modify: all affected `examples/**/README.md`
- Create: `test/examples/examples_schema_test.go`

- [ ] **Step 1: Replace stale example fields**

  Update examples:

  - `image.imagePullPolicy` -> `image.pullPolicy`
  - remove unsupported instance `imagePullSecrets`
  - `workspace.files` -> `workspace.initialFiles`
  - `security.serviceAccount` -> `security.rbac`
  - `networking.networkPolicy` -> `security.networkPolicy`
  - `availability.pdb` -> `availability.podDisruptionBudget`
  - `availability.hpa` -> `availability.horizontalPodAutoscaler`
  - remove `runtime.extraApt` and `runtime.extraAptPackages`
  - `runtime.extraPip` -> `runtime.extraPipPackages`
  - `profileStore.enabled` -> `profileStore.honcho.enabled`
  - remove unsupported `webTerminal` and `tailscale`

- [ ] **Step 2: Fix gateway Secret refs**

  Use actual fields:

  - `botTokenSecretRef`
  - `appTokenSecretRef`
  - `signingSecretRef`
  - `providerSecretRef`
  - `phoneNumberSecretRef`
  - `authTokenSecretRef`

- [ ] **Step 3: Align backup docs with raw format**

  Docs must describe raw `.tar.zst` S3 objects, not restic repo layout, tags, or restic prune behavior.

- [ ] **Step 4: Add strict example schema test**

  Add a Go test that:

  - Finds Hermes CR YAML files under `examples/`.
  - Uses strict YAML decoding.
  - Fails on unknown fields.
  - Decodes into `api/v1` types by `apiVersion` and `kind`.

- [ ] **Step 5: Verify Task 8.3**

  Run:

  ```bash
  go test ./test/examples/...
  rg 'imagePullPolicy|imagePullSecrets|workspace\.files|extraApt:|extraPip:|webTerminal|tailscale|profileStore:\n  enabled|availability:\n    pdb|secretRef:' examples README.md docs/api-reference.md
  ```

### Task 8.4: Release Gates And Conformance

**Files:**

- Modify: `.github/workflows/conformance.yaml`
- Modify: `.github/workflows/release.yaml`
- Modify: `.github/workflows/release-please.yaml`
- Modify: `.github/workflows/operatorhub-submit.yaml`
- Modify: `docs/release-process.md`
- Modify: `.github/pull_request_template.md`

- [ ] **Step 1: Expand conformance triggers**

  Trigger conformance on changes to:

  - `api/v1/**`
  - `internal/controller/**`
  - `internal/resources/**`
  - `internal/webhook/**`
  - `cmd/**`
  - `config/**`
  - `charts/**`
  - `bundle/**`
  - `Makefile`
  - `go.mod`
  - `go.sum`
  - relevant workflows

- [ ] **Step 2: Add workflow-call conformance**

  Add `workflow_call` to `conformance.yaml` so `release.yaml` can call the same suite.

- [ ] **Step 3: Gate release publishing**

  In `release.yaml`, make publish jobs depend on:

  - `make test-readonly`
  - `bash hack/check-distribution-parity.sh`
  - `make bundle-validate`
  - RBAC drift checks
  - conformance workflow
  - dirty-tree check after generated artifacts

- [ ] **Step 4: Verify Task 8.4**

  Run locally where possible:

  ```bash
  make test-readonly
  bash hack/check-distribution-parity.sh
  make bundle-validate
  make conformance-negative
  make conformance-idempotency
  make conformance-gitops
  make conformance-failure
  make conformance-upgrade
  ```

## Phase 9: Supply Chain Hardening

**Purpose:** Reduce mutable dependency and release drift.

**Parallelism:** One supply-chain worker. It can run in parallel with docs, but final workflow verification waits for release-gate changes.

**Files:**

- Modify: `api/v1/hermesinstance_types.go`
- Modify: `internal/resources/statefulset.go`
- Modify: `Dockerfile`
- Modify: `images/hermes-agent/Dockerfile`
- Modify: `Makefile`
- Modify: `.github/workflows/*.yaml`
- Modify: `.github/dependabot.yml`
- Create or update: `images/hermes-agent/uv.lock`

- [ ] **Step 1: Remove runtime default to mutable `latest`**

  Change defaulting/validation so `spec.image.tag` must be explicit and not `latest`, unless image is specified by digest.

  Update `imageRef` so it does not silently substitute `latest`.

- [ ] **Step 2: Pin base images by digest**

  Update:

  - root `Dockerfile`
  - `images/hermes-agent/Dockerfile`

  Use digest-pinned base references and document the update flow in `docs/development.md`.

- [ ] **Step 3: Pin workflow actions and tools**

  Replace tag-only workflow uses with SHA-pinned actions where practical. Replace `version: latest` for GoReleaser/Helm tooling with explicit versions.

- [ ] **Step 4: Pin relock tooling**

  In `Makefile`, pin the uv image used by `agent-image-relock` by version and digest.

- [ ] **Step 5: Verify Phase 9**

  Run:

  ```bash
  rg -n 'version: latest|:latest|uses: .*@[vV]?[0-9]' .github Dockerfile images/hermes-agent api/v1 internal/resources
  make agent-image-relock HERMES_VERSION=v0.13.0
  docker buildx build --platform linux/amd64 --load -t hermes-agent:security-smoke images/hermes-agent
  ```

## Phase 10: Generated Artifacts And Integration

**Purpose:** Regenerate all derived outputs once and prove the repository is consistent.

**Parallelism:** One integration agent only.

**Files:**

- Generated: `api/v1/zz_generated.deepcopy.go`
- Generated: `config/crd/bases/*.yaml`
- Generated/synced: `charts/hermes-operator/templates/crds/*.yaml`
- Generated: `config/rbac/role.yaml`
- Updated: `bundle/manifests/*.yaml`
- Updated: `dist/install.yaml` if generated for release only

- [ ] **Step 1: Regenerate Go and manifests**

  Run:

  ```bash
  make generate manifests sync-chart-crds
  ```

- [ ] **Step 2: Update bundle**

  Run:

  ```bash
  make bundle VERSION=0.1.8 IMG=ghcr.io/paperclipinc/hermes-operator:v0.1.8
  bash hack/sync-bundle-rbac.sh --check
  make bundle-validate
  ```

- [ ] **Step 3: Verify webhook and CEL artifacts**

  Run:

  ```bash
  kubectl kustomize config/default | rg 'ValidatingWebhookConfiguration|MutatingWebhookConfiguration'
  rg -n 'x-kubernetes-validations|profileID|bucket|endpoint|key' config/crd/bases charts/hermes-operator/templates/crds
  ```

- [ ] **Step 4: Run full local verification**

  Run:

  ```bash
  make test-readonly
  make test-controller
  CGO_ENABLED=0 go test -run '^$' ./...
  helm template hermes-operator charts/hermes-operator >/tmp/hermes-operator-helm.yaml
  kubectl kustomize config/default >/tmp/hermes-operator-kustomize.yaml
  bash hack/check-helm-rbac.sh
  bash hack/check-distribution-parity.sh
  git status --short
  ```

## Phase 11: Guarded Live Kubernetes Smoke

**Purpose:** Validate install-time behavior against a real cluster without allowing test workload egress.

**Parallelism:** One final verification agent. Only run after local integration passes.

**Files:**

- No source files expected.
- Temporary Kubernetes namespace and Cilium policies only.

- [ ] **Step 1: Create an ephemeral namespace**

  Use a unique name:

  ```bash
  NS=hermes-audit-$(date +%Y%m%d%H%M%S)
  kubectl create namespace "$NS"
  ```

- [ ] **Step 2: Apply Cilium egress deny before workloads**

  Apply a namespace-scoped CiliumNetworkPolicy with:

  ```yaml
  endpointSelector: {}
  egressDeny:
    - toEntities:
        - all
  ```

- [ ] **Step 3: Install operator test artifacts**

  Install into the ephemeral namespace only when manifests and RBAC are ready. If the operator still requires cluster-scoped CRDs/RBAC, document the exact cluster-scoped objects and get explicit approval before applying them.

- [ ] **Step 4: Smoke a minimal `HermesInstance`**

  Create a minimal instance using an image already present in the cluster or preloaded into kind. Verify:

  - CR is admitted by webhooks.
  - PVC behavior matches `persistence.enabled`.
  - Service/metrics ports match pod ports.
  - Status does not report Ready until required subsystems are actually ready.
  - No egress is allowed from pods in the test namespace.

- [ ] **Step 5: Cleanup**

  Run:

  ```bash
  kubectl delete namespace "$NS" --wait=true
  ```

## Final Acceptance Gate

The remediation is complete only when all of these are true:

- Command injection tests fail on unsafe profile IDs and S3 fields.
- `storage.persistence.enabled=false` uses `emptyDir` and creates no PVC.
- Auto-update desired image is not overwritten by parent reconciliation.
- Backup/restore/migration use raw `.tar.zst` S3 objects and no restic commands.
- SelfConfig `contentFrom` works or is rejected with a clear admission error.
- Status `Ready` depends on active subsystem health, not only StatefulSet replicas.
- Day-2 subcontroller errors and requeues propagate to the main reconcile result.
- Helm and kustomize both render webhooks, metrics args, probes, and metrics auth RBAC.
- OLM metadata has valid semver names/images and bundle validation passes.
- Examples strictly decode against current API types.
- Release workflow gates publishing on conformance and distribution checks.
- Supply-chain scan finds no mutable `latest` defaults in runtime paths.
- Guarded live smoke runs in an ephemeral namespace with Cilium egress deny and cleans up.

## Recommended Commit Order

1. `test: stabilize read-only and envtest verification`
2. `fix: block unsafe selfconfig and migration shell inputs`
3. `fix: enforce admission guardrails for workload passthrough`
4. `fix: wire persistence metrics honcho and workspace builders`
5. `fix: propagate controller readiness errors and requeues`
6. `fix: make autoupdate desired state reconciler-owned`
7. `fix: use raw s3 tar zstd backup restore format`
8. `feat: support selfconfig workspace contentFrom`
9. `fix: align helm kustomize and olm distribution artifacts`
10. `docs: sync examples api docs and backup format`
11. `ci: gate release on conformance and distribution checks`
12. `build: pin supply-chain inputs and agent lockfile`
13. `test: add guarded live cluster smoke evidence`
