package conformance

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// negativeCase describes one webhook-deny scenario exercised via kubectl apply.
type negativeCase struct {
	// name is a short human-readable label for the test row.
	name string
	// yaml is the HermesInstance manifest to apply.
	yaml string
	// wantErrSubstring is a substring expected in the kubectl apply error output.
	wantErrSubstring string
	// isUpdate: when true, apply the base yaml first, then apply yaml as an update.
	isUpdate bool
	// baseYAML is applied first when isUpdate=true; yaml is then the mutation.
	baseYAML string
	// skip marks cases not yet supported by the live webhook infrastructure.
	skip string
}

// hermesInstanceYAML is a helper that builds a minimal valid HermesInstance
// manifest with the given name, then merges extra YAML fields inline.
func hermesInstanceYAML(name, extra string) string {
	base := fmt.Sprintf(`apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: %s
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
`, name)
	if extra == "" {
		return base
	}
	// Indent extra under spec:: caller must pass only spec-level fields.
	lines := strings.Split(extra, "\n")
	indented := make([]string, 0, len(lines))
	for _, l := range lines {
		if l != "" {
			indented = append(indented, "  "+l)
		}
	}
	return base + strings.Join(indented, "\n") + "\n"
}

var negativeCases = []negativeCase{
	// ── image.repository required ──────────────────────────────────────────────
	{
		name: "deny: image.repository empty",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-no-image-repo
spec:
  image:
    repository: ""
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
`,
		wantErrSubstring: "image.repository",
	},

	// ── storage.persistence.size required ──────────────────────────────────────
	{
		name: `deny: storage.persistence.size empty`,
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-no-storage-size
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: ""
`,
		wantErrSubstring: "storage.persistence.size",
	},

	// ── selfConfigure.enabled=true requires protectedKeys ─────────────────────
	{
		name: "deny: selfConfigure enabled without protectedKeys",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-selfconfig-no-keys
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  selfConfigure:
    enabled: true
    allowedActions:
      - skills
`,
		wantErrSubstring: "protectedKeys",
	},

	// ── selfConfigure.enabled=true requires allowedActions ────────────────────
	{
		name: "deny: selfConfigure enabled without allowedActions",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-selfconfig-no-actions
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  selfConfigure:
    enabled: true
    protectedKeys:
      - image
`,
		wantErrSubstring: "allowedActions",
	},

	// ── selfConfigure unknown action ──────────────────────────────────────────
	{
		name: "deny: selfConfigure unknown action value",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-selfconfig-bad-action
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  selfConfigure:
    enabled: true
    protectedKeys:
      - image
    allowedActions:
      - reboot-cluster
`,
		wantErrSubstring: "reboot-cluster",
	},

	// ── PDB MinAvailable + MaxUnavailable mutually exclusive ──────────────────
	{
		name: "deny: PDB minAvailable and maxUnavailable both set",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-pdb-both
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  availability:
    podDisruptionBudget:
      enabled: true
      minAvailable: "50%"
      maxUnavailable: "1"
`,
		wantErrSubstring: "podDisruptionBudget",
	},

	// ── HPA minReplicas > maxReplicas ─────────────────────────────────────────
	{
		name: "deny: HPA minReplicas > maxReplicas",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-hpa-bad-range
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  availability:
    horizontalPodAutoscaler:
      enabled: true
      minReplicas: 10
      maxReplicas: 2
`,
		wantErrSubstring: "horizontalPodAutoscaler",
	},

	// ── restoreFrom + migration mutually exclusive ────────────────────────────
	{
		name: "deny: restoreFrom and migration.fromOpenClaw both set",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-restore-migration
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  restoreFrom: snapshot-abc123
  migration:
    fromOpenClaw:
      mode: copy
      source:
        openclawInstanceRef:
          name: old-openclaw
          namespace: default
`,
		wantErrSubstring: "spec",
	},

	// ── migration source exactly-one (both set) ───────────────────────────────
	{
		name: "deny: migration source openclawInstanceRef and backupRef both set",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-migration-both-sources
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  migration:
    fromOpenClaw:
      mode: copy
      source:
        openclawInstanceRef:
          name: old-openclaw
          namespace: default
        backupRef:
          s3:
            bucket: my-bucket
            endpoint: s3.example.com
            key: backup-key
            credentialsSecretRef:
              name: s3-creds
`,
		wantErrSubstring: "source",
	},

	// ── migration source neither set ─────────────────────────────────────────
	{
		name: "deny: migration source neither openclawInstanceRef nor backupRef",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-migration-no-source
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  migration:
    fromOpenClaw:
      mode: copy
      source: {}
`,
		wantErrSubstring: "source",
	},

	// -- migration S3 rejects unsafe object keys ------------------------------
	{
		name: "deny: migration S3 key path traversal",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-migration-s3-key-traversal
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  migration:
    fromOpenClaw:
      mode: copy
      source:
        backupRef:
          s3:
            bucket: my-bucket
            endpoint: s3.example.com
            key: ../backup.tar.zst
            credentialsSecretRef:
              name: s3-creds
`,
		wantErrSubstring: "key",
	},

	// -- migration S3 rejects unsafe endpoints --------------------------------
	{
		name: "deny: migration S3 endpoint shell metacharacter",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-migration-s3-endpoint-shell
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  migration:
    fromOpenClaw:
      mode: copy
      source:
        backupRef:
          s3:
            bucket: my-bucket
            endpoint: s3.example.com;rm
            key: backup.tar.zst
            credentialsSecretRef:
              name: s3-creds
`,
		wantErrSubstring: "endpoint",
	},

	// -- runtime.extraAptPackages is unsupported -----------------------------
	{
		name: "deny: runtime extraAptPackages",
		yaml: hermesInstanceYAML("neg-extra-apt", `runtime:
  extraAptPackages:
    - curl`),
		wantErrSubstring: "custom agent image",
	},

	// -- unsafe workload passthrough ------------------------------------------
	{
		name: "deny: sidecar privileged security context",
		yaml: hermesInstanceYAML("neg-sidecar-privileged", `sidecars:
  - name: debug
    image: busybox:1.36.1
    securityContext:
      privileged: true`),
		wantErrSubstring: "privileged",
	},
	{
		name: "deny: extraVolumes hostPath",
		yaml: hermesInstanceYAML("neg-hostpath", `extraVolumes:
  - name: host
    hostPath:
      path: /var/lib/kubelet`),
		wantErrSubstring: "hostPath",
	},
	{
		name: "deny: extraVolumeMounts docker socket",
		yaml: hermesInstanceYAML("neg-docker-sock", `extraVolumeMounts:
  - name: docker
    mountPath: /var/run/docker.sock`),
		wantErrSubstring: "docker.sock",
	},

	// -- unsafe network exposure ---------------------------------------------
	{
		name: "deny: NodePort service",
		yaml: hermesInstanceYAML("neg-nodeport", `networking:
  service:
    type: NodePort`),
		wantErrSubstring: "NodePort",
	},
	{
		name: "deny: LoadBalancer service",
		yaml: hermesInstanceYAML("neg-loadbalancer", `networking:
  service:
    type: LoadBalancer`),
		wantErrSubstring: "LoadBalancer",
	},
	{
		name: "deny: ingress enabled without TLS",
		yaml: hermesInstanceYAML("neg-ingress-no-tls", `networking:
  ingress:
    enabled: true
    host: hermes.example.com`),
		wantErrSubstring: "TLS",
	},
	{
		name: "deny: ingress disables TLS redirect",
		yaml: hermesInstanceYAML("neg-ingress-no-redirect", `networking:
  ingress:
    enabled: true
    host: hermes.example.com
    tls:
      - secretName: hermes-tls
    annotations:
      nginx.ingress.kubernetes.io/ssl-redirect: "false"`),
		wantErrSubstring: "ssl-redirect",
	},
	{
		name: "deny: metrics enabled with secure false",
		yaml: hermesInstanceYAML("neg-metrics-insecure", `observability:
  metrics:
    enabled: true
    secure: false`),
		wantErrSubstring: "secure",
	},

	// ── immutable: storageClassName changed ──────────────────────────────────
	{
		name:     "deny: storage.persistence.storageClassName changed (immutable)",
		isUpdate: true,
		baseYAML: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-immutable-sc
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
      storageClassName: gp3
`,
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-immutable-sc
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
      storageClassName: io2
`,
		wantErrSubstring: "storageClassName",
	},

	// ── telegram gateway: enabled without botTokenSecretRef ──────────────────
	{
		name: "deny: telegram gateway enabled without botTokenSecretRef",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-telegram-no-secret
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  gateways:
    telegram:
      enabled: true
`,
		wantErrSubstring: "botTokenSecretRef",
	},

	// ── discord gateway: enabled without botTokenSecretRef ───────────────────
	{
		name: "deny: discord gateway enabled without botTokenSecretRef",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-discord-no-secret
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  gateways:
    discord:
      enabled: true
`,
		wantErrSubstring: "botTokenSecretRef",
	},

	// ── slack gateway: enabled without botTokenSecretRef ─────────────────────
	{
		name: "deny: slack gateway enabled without botTokenSecretRef",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-slack-no-secret
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  gateways:
    slack:
      enabled: true
`,
		wantErrSubstring: "botTokenSecretRef",
	},

	// ── whatsapp gateway: enabled without providerSecretRef ──────────────────
	{
		name: "deny: whatsapp gateway enabled without providerSecretRef",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-whatsapp-no-secret
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  gateways:
    whatsapp:
      enabled: true
`,
		wantErrSubstring: "providerSecretRef",
	},

	// ── signal gateway: enabled without phoneNumberSecretRef ─────────────────
	{
		name: "deny: signal gateway enabled without phoneNumberSecretRef",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-signal-no-phone
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  gateways:
    signal:
      enabled: true
`,
		wantErrSubstring: "phoneNumberSecretRef",
	},

	// ── config.raw + configMapRef both set without mergeMode (warning only) ───
	// This case emits a warning, not a denial. We assert no hard error but do
	// verify the apply succeeds (round-trip test).
	{
		name: "warn (not deny): config.raw and configMapRef both set without mergeMode",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: neg-config-both-warn
spec:
  image:
    repository: ghcr.io/paperclipinc/hermes-agent
    tag: v2026.5.29.2
  storage:
    persistence:
      enabled: true
      size: 1Gi
  config:
    raw: {}
    configMapRef:
      name: some-config
`,
		// The webhook emits a warning but does NOT deny. We set wantErrSubstring
		// empty to indicate success is expected: the test structure will use
		// this sentinel to skip the error assertion.
		wantErrSubstring: "",
	},

	// ── HermesSelfConfig: instanceRef points to non-existent HermesInstance ──
	{
		name: "deny: HermesSelfConfig instanceRef references missing HermesInstance",
		yaml: `apiVersion: hermes.agent/v1
kind: HermesSelfConfig
metadata:
  name: neg-selfconfig-missing-parent
spec:
  instanceRef: does-not-exist
  addSkills:
    - source: hermes-skill-example
`,
		wantErrSubstring: "instanceRef",
	},

	// HermesSelfConfig: addEnvVars value and valueFrom are mutually exclusive.
	{
		name:     "deny: HermesSelfConfig addEnvVars value and valueFrom",
		isUpdate: false,
		baseYAML: hermesInstanceYAML("neg-parent-env-xor", ""),
		yaml: fmt.Sprintf(`apiVersion: hermes.agent/v1
kind: HermesSelfConfig
metadata:
  name: neg-selfconfig-env-xor
spec:
  instanceRef: %s
  addEnvVars:
    - name: TOKEN
      value: literal
      valueFrom:
        secretKeyRef:
          name: tokens
          key: token
`, "neg-parent-env-xor"),
		wantErrSubstring: "valueFrom",
	},

	// HermesSelfConfig: addWorkspaceFiles content and contentFrom are exclusive.
	{
		name:     "deny: HermesSelfConfig addWorkspaceFiles content and contentFrom",
		isUpdate: false,
		baseYAML: hermesInstanceYAML("neg-parent-workspace-xor", ""),
		yaml: fmt.Sprintf(`apiVersion: hermes.agent/v1
kind: HermesSelfConfig
metadata:
  name: neg-selfconfig-workspace-xor
spec:
  instanceRef: %s
  addWorkspaceFiles:
    - path: notes/secure.md
      content: literal
      contentFrom:
        name: workspace
        key: secure.md
`, "neg-parent-workspace-xor"),
		wantErrSubstring: "contentFrom",
	},

	// ── HermesSelfConfig: addProfileSnapshot without honcho enabled ───────────
	// This case requires a parent HermesInstance without profileStore.honcho enabled.
	{
		name:     "deny: HermesSelfConfig addProfileSnapshot without honcho enabled on parent",
		isUpdate: false,
		baseYAML: hermesInstanceYAML("neg-parent-no-honcho", ""),
		yaml: fmt.Sprintf(`apiVersion: hermes.agent/v1
kind: HermesSelfConfig
metadata:
  name: neg-selfconfig-profile-no-honcho
spec:
  instanceRef: %s
  addProfileSnapshot:
    profileID: my-profile
    data: '{"preferences":{}}'
`, "neg-parent-no-honcho"),
		wantErrSubstring: "profileStore.honcho",
	},

	// HermesSelfConfig: addProfileSnapshot profileID must be path-safe.
	{
		name:     "deny: HermesSelfConfig addProfileSnapshot profileID shell metacharacters",
		isUpdate: false,
		baseYAML: hermesInstanceYAML("neg-parent-profile-shell", `profileStore:
  honcho:
    enabled: true
    apiKeySecretRef:
      name: honcho-api
      key: api-key`),
		yaml: fmt.Sprintf(`apiVersion: hermes.agent/v1
kind: HermesSelfConfig
metadata:
  name: neg-selfconfig-profile-shell
spec:
  instanceRef: %s
  addProfileSnapshot:
    profileID: "prod; touch /tmp/pwned"
    data: '{"preferences":{}}'
`, "neg-parent-profile-shell"),
		wantErrSubstring: "profileID",
	},

	// HermesSelfConfig: addProfileSnapshot profileID cannot traverse paths.
	{
		name:     "deny: HermesSelfConfig addProfileSnapshot profileID path traversal",
		isUpdate: false,
		baseYAML: hermesInstanceYAML("neg-parent-profile-path", `profileStore:
  honcho:
    enabled: true
    apiKeySecretRef:
      name: honcho-api
      key: api-key`),
		yaml: fmt.Sprintf(`apiVersion: hermes.agent/v1
kind: HermesSelfConfig
metadata:
  name: neg-selfconfig-profile-path
spec:
  instanceRef: %s
  addProfileSnapshot:
    profileID: "../prod"
    data: '{"preferences":{}}'
`, "neg-parent-profile-path"),
		wantErrSubstring: "profileID",
	},
}

var _ = Describe("webhook deny paths", Ordered, func() {
	var ns string

	BeforeAll(func() {
		ns = freshNamespace("neg-test")
		DeferCleanup(func() {
			deleteNamespace(ns)
		})
	})

	for _, tc := range negativeCases {
		tc := tc // capture loop variable

		It(tc.name, func() {
			if tc.skip != "" {
				Skip(tc.skip)
			}

			// For cases that need a pre-existing parent object, apply it first.
			if tc.baseYAML != "" && !tc.isUpdate {
				out, err := kubectlApply(addNamespace(tc.baseYAML, ns))
				Expect(err).ToNot(HaveOccurred(), "applying base fixture: %s", out)
				DeferCleanup(func() {
					_, _ = kubectlDelete(addNamespace(tc.baseYAML, ns))
				})
			}

			// For immutable-field update tests: apply base, then mutate.
			if tc.isUpdate {
				out, err := kubectlApply(addNamespace(tc.baseYAML, ns))
				Expect(err).ToNot(HaveOccurred(), "applying base for update test: %s", out)
				DeferCleanup(func() {
					_, _ = kubectlDelete(addNamespace(tc.baseYAML, ns))
				})
			}

			// Empty wantErrSubstring means "expect success (warning path)".
			if tc.wantErrSubstring == "" {
				out, err := kubectlApply(addNamespace(tc.yaml, ns))
				Expect(err).ToNot(HaveOccurred(), "expected success but got error: %s", out)
				DeferCleanup(func() {
					_, _ = kubectlDelete(addNamespace(tc.yaml, ns))
				})
				return
			}

			_, err := kubectlApply(addNamespace(tc.yaml, ns))
			Expect(err).To(HaveOccurred(), "expected webhook denial but apply succeeded")
			Expect(err.Error()).To(ContainSubstring(tc.wantErrSubstring),
				"error message should mention %q", tc.wantErrSubstring)
		})
	}
})

// addNamespace rewrites `namespace: <placeholder>` or injects a namespace
// into every resource that lacks one. Simple string injection for test fixtures.
func addNamespace(yaml, ns string) string {
	// Insert namespace under metadata if not present.
	result := strings.ReplaceAll(yaml, "\nmetadata:\n  name:", "\nmetadata:\n  namespace: "+ns+"\n  name:")
	return result
}
