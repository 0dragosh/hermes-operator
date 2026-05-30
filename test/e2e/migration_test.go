//go:build migration
// +build migration

package e2e

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Migration (build-tag: migration): openclaw -> hermes", func() {
	It("imports a sibling OpenClawInstance via the in-cluster ref path", func() {
		cfg := e2eConfigFromEnv()
		ns := cfg.WorkloadNamespace
		out, err := kubectl("get", "openclawinstance/oc-source", "-n", ns)
		if err != nil {
			Skip("oc-source OpenClawInstance not present in " + ns + "; skipping (run: kubectl apply -f hack/migration-fixtures/ into the e2e workload namespace)")
		}
		_ = out

		manifest, err := renderE2ETemplate(`
apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: hermes-from-oc
  namespace: {{ .WorkloadNamespace }}
spec:
  image:
    repository: {{ .AgentImageRepository }}
    tag: "{{ .AgentImageTag }}"
  storage:
    persistence:
      enabled: true
      size: 1Gi
  migration:
    fromOpenClaw:
      mode: copy
      source:
        openclawInstanceRef:
          name: oc-source
          namespace: {{ .WorkloadNamespace }}
`, cfg)
		Expect(err).NotTo(HaveOccurred(), "render migration manifest")

		out, err = runStdin("kubectl", []string{"apply", "-f", "-"}, manifest)
		Expect(err).ToNot(HaveOccurred(), "apply: %s", out)

		Eventually(func() string {
			out, _ := kubectl("get", "hermesinstance/hermes-from-oc", "-n", ns, "-o", "jsonpath={.status.migration.completed}")
			return strings.TrimSpace(out)
		}, 10*time.Minute).Should(Equal("true"))

		Eventually(func() string {
			out, _ := kubectl(
				"get", "hermesinstance/hermes-from-oc", "-n", ns,
				"-o", `jsonpath={.status.conditions[?(@.type=="MigrationCompleted")].status}`)
			return strings.TrimSpace(out)
		}).Should(Equal("True"))
	})
})
