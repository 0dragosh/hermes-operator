package e2e

import (
	"encoding/json"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

var _ = Describe("HermesInstance with Telegram + Honcho on kind", Ordered, func() {
	var manifest string
	var cfg e2eConfig

	BeforeAll(func() {
		if os.Getenv("HERMES_E2E_FULL") != "1" {
			Skip("set HERMES_E2E_FULL=1 to enable the Plan-3 gateway+honcho e2e (requires the agent image to be published)")
		}
		cfg = e2eConfigFromEnv()
		var err error
		manifest, err = renderE2ETemplateFile("testdata/hermesinstance-gateways.yaml", cfg)
		Expect(err).NotTo(HaveOccurred(), "render gateway manifest")

		out, err := runStdin("kubectl", []string{"apply", "-f", "-"}, manifest)
		Expect(err).NotTo(HaveOccurred(), out)
	})

	AfterAll(func() {
		if os.Getenv("HERMES_E2E_FULL") != "1" {
			return
		}
		_, _ = runStdin("kubectl", []string{"delete", "--ignore-not-found=true", "-f", "-"}, manifest)
	})

	It("brings the hermes pod to Ready", func() {
		Eventually(func(g Gomega) {
			out, err := kubectl("get", "statefulset", "e2e-gateways",
				"-n", cfg.WorkloadNamespace,
				"-o", "jsonpath={.status.readyReplicas}")
			g.Expect(err).NotTo(HaveOccurred(), out)
			g.Expect(strings.TrimSpace(out)).To(Equal("1"))
		}).Should(Succeed())
	})

	It("brings the Honcho Deployment to Ready", func() {
		Eventually(func(g Gomega) {
			out, err := kubectl("get", "deployment", "e2e-gateways-honcho",
				"-n", cfg.WorkloadNamespace,
				"-o", "jsonpath={.status.readyReplicas}")
			g.Expect(err).NotTo(HaveOccurred(), out)
			g.Expect(strings.TrimSpace(out)).To(Equal("1"))
		}).Should(Succeed())
	})

	It("does not emit a broad 443/TCP egress rule when gateways are enabled", func() {
		out, err := kubectl("get", "networkpolicy", "e2e-gateways",
			"-n", cfg.WorkloadNamespace,
			"-o", "json")
		Expect(err).NotTo(HaveOccurred(), out)
		var policy networkingv1.NetworkPolicy
		Expect(json.Unmarshal([]byte(out), &policy)).To(Succeed(), out)
		Expect(hasBroadTCP443Egress(policy.Spec.Egress)).To(BeFalse())
	})

	It("emits a Honcho-scoped NetworkPolicy with ingress only from the hermes pod", func() {
		out, err := kubectl("get", "networkpolicy", "e2e-gateways-honcho",
			"-n", cfg.WorkloadNamespace,
			"-o", `jsonpath={.spec.ingress[*].from[*].podSelector.matchLabels.app\.kubernetes\.io/name}`)
		Expect(err).NotTo(HaveOccurred(), out)
		Expect(strings.TrimSpace(out)).To(Equal("hermes-agent"))
	})
})

func hasBroadTCP443Egress(rules []networkingv1.NetworkPolicyEgressRule) bool {
	for _, rule := range rules {
		if len(rule.To) != 0 {
			continue
		}
		if len(rule.Ports) == 0 {
			return true
		}
		for _, port := range rule.Ports {
			if port.Protocol != nil && *port.Protocol != corev1.ProtocolTCP {
				continue
			}
			if port.Port != nil && port.Port.IntValue() == 443 {
				return true
			}
		}
	}
	return false
}
