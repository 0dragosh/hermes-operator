package e2e

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "hermes-operator e2e suite")
}

var execCommand = exec.Command

var _ = BeforeSuite(func() {
	cfg := e2eConfigFromEnv()
	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)
	By("ensuring e2e namespaces exist")
	ensureE2ENamespaces(cfg)

	By("installing CRDs via helm chart")
	out, err := run("helm", "upgrade", "--install", "hermes-operator", "../../charts/hermes-operator",
		"--namespace", cfg.OperatorNamespace, "--create-namespace",
		"--set-string", "image.repository="+cfg.OperatorImageRepository,
		"--set-string", "image.tag="+cfg.OperatorImageTag,
		"--set-string", "image.pullPolicy="+cfg.OperatorImagePullPolicy,
		"--wait", "--timeout=10m")
	if err != nil {
		desc, _ := kubectl("describe", "deploy/hermes-operator", "-n", cfg.OperatorNamespace)
		pods, _ := kubectl("get", "pods", "-n", cfg.OperatorNamespace, "-o", "wide")
		logs, _ := kubectl("logs", "-l", "app.kubernetes.io/name=hermes-operator", "-n", cfg.OperatorNamespace, "--all-containers=true", "--tail=200")
		Fail("helm upgrade failed: " + out + "\n\n--- deploy describe ---\n" + desc + "\n\n--- pods ---\n" + pods + "\n\n--- operator logs ---\n" + logs)
	}
	By("waiting for operator webhook endpoint to have a Ready backend")
	Eventually(func() string {
		out, _ := kubectl("get", "endpoints/hermes-operator-webhook", "-n", cfg.OperatorNamespace,
			"-o", "jsonpath={.subsets[0].addresses[0].ip}")
		return strings.TrimSpace(out)
	}, 3*time.Minute, 5*time.Second).ShouldNot(BeEmpty(),
		"operator webhook backend never became ready; helm --wait passed but the validating-webhook service has no endpoints")

	By("waiting for the validating webhook to actually answer (cert injection + TLS bind can lag past pod-ready)")
	probe, err := renderE2ETemplate(`apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: e2e-webhook-probe
  namespace: {{ .WorkloadNamespace }}
spec:
  image:
    repository: {{ .AgentImageRepository }}
    tag: "{{ .AgentImageTag }}"
  storage:
    persistence:
      enabled: true
      size: 1Gi
`, cfg)
	Expect(err).NotTo(HaveOccurred(), "render webhook probe")
	deadline := time.Now().Add(5 * time.Minute)
	var lastErr string
	for {
		out, err := runStdin("kubectl", []string{"apply", "--dry-run=server", "-f", "-"}, probe)
		if err == nil {
			break
		}
		lastErr = strings.TrimSpace(out)
		if time.Now().After(deadline) {
			desc, _ := kubectl("describe", "validatingwebhookconfigurations.admissionregistration.k8s.io")
			mut, _ := kubectl("describe", "mutatingwebhookconfigurations.admissionregistration.k8s.io")
			certs, _ := kubectl("get", "certificates,certificaterequests,secrets", "-n", cfg.OperatorNamespace, "-o", "wide")
			pods, _ := kubectl("get", "pods", "-n", cfg.OperatorNamespace, "-o", "wide")
			logs, _ := kubectl("logs", "-n", cfg.OperatorNamespace, "-l", "app.kubernetes.io/name=hermes-operator", "--all-containers=true", "--tail=200")
			Fail("webhook never answered a dry-run apply within 5m. last error:\n" + lastErr +
				"\n\n--- validatingwebhookconfigs ---\n" + desc +
				"\n--- mutatingwebhookconfigs ---\n" + mut +
				"\n--- certs+secrets ---\n" + certs +
				"\n--- pods ---\n" + pods +
				"\n--- operator logs ---\n" + logs)
		}
		time.Sleep(3 * time.Second)
	}

	By("installing MinIO for backup/restore e2e")
	InstallMinIO(cfg)
	CreateHermesS3CredsSecret(cfg.WorkloadNamespace)
	By("applying least-access NetworkPolicies for e2e namespaces")
	ApplyE2ENetworkPolicies(cfg)
})

var _ = AfterSuite(func() {
	cleanupE2EResources(e2eConfigFromEnv())
})

func run(cmd string, args ...string) (string, error) {
	c := execCommand(cmd, args...)
	b, err := c.CombinedOutput()
	return string(b), err
}

func kubectl(args ...string) (string, error) {
	return run("kubectl", args...)
}

func runStdin(cmd string, args []string, stdin string) (string, error) {
	c := execCommand(cmd, args...)
	c.Stdin = strings.NewReader(stdin)
	b, err := c.CombinedOutput()
	return string(b), err
}

func ensureE2ENamespaces(cfg e2eConfig) {
	for _, namespace := range uniqueNamespaces(cfg.OperatorNamespace, cfg.WorkloadNamespace, cfg.MinIONamespace) {
		if strings.TrimSpace(namespace) == "" {
			Fail("e2e namespace must not be empty")
		}
		if _, err := kubectl("get", "namespace", namespace); err == nil {
			continue
		}
		out, err := kubectl("create", "namespace", namespace)
		Expect(err).NotTo(HaveOccurred(), "create namespace %s: %s", namespace, out)
	}
}

func uniqueNamespaces(namespaces ...string) []string {
	seen := make(map[string]bool, len(namespaces))
	unique := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		if seen[namespace] {
			continue
		}
		seen[namespace] = true
		unique = append(unique, namespace)
	}
	return unique
}

func ApplyE2ENetworkPolicies(cfg e2eConfig) {
	supported, diagnostic := clusterSupportsNetworkPolicy()
	if !supported {
		GinkgoWriter.Printf("networking.k8s.io/v1 NetworkPolicy is unavailable; skipping e2e NetworkPolicies. api-resources output:\n%s\n", diagnostic)
		return
	}

	out, err := runStdin("kubectl", []string{"apply", "-f", "-"}, e2eNetworkPolicyManifest(cfg))
	Expect(err).NotTo(HaveOccurred(), "apply e2e NetworkPolicies: %s", out)
}

func clusterSupportsNetworkPolicy() (bool, string) {
	out, err := kubectl("api-resources", "--api-group=networking.k8s.io", "-o", "name")
	if err != nil {
		return false, out
	}
	for _, line := range strings.Split(out, "\n") {
		resource := strings.TrimSpace(line)
		if resource == "networkpolicies" || resource == "networkpolicies.networking.k8s.io" {
			return true, out
		}
	}
	return false, out
}

func cleanupE2EResources(cfg e2eConfig) {
	for _, command := range e2eCleanupCommands(cfg) {
		_, _ = run(command.name, command.args...)
	}
}
