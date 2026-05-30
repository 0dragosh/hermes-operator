package e2e

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

func TestE2EConfigDefaultsAndOverrides(t *testing.T) {
	defaults := newE2EConfigFromEnv(func(string) string { return "" })
	if defaults.OperatorNamespace != "hermes-system" {
		t.Fatalf("default operator namespace = %q, want hermes-system", defaults.OperatorNamespace)
	}
	if defaults.WorkloadNamespace != "default" {
		t.Fatalf("default workload namespace = %q, want default", defaults.WorkloadNamespace)
	}
	if defaults.MinIONamespace != "minio" {
		t.Fatalf("default MinIO namespace = %q, want minio", defaults.MinIONamespace)
	}
	if defaults.OperatorImageRepository != "hermes-operator" || defaults.OperatorImageTag != "dev" || defaults.OperatorImagePullPolicy != "IfNotPresent" {
		t.Fatalf("default operator image = %q:%q/%q, want hermes-operator:dev/IfNotPresent",
			defaults.OperatorImageRepository, defaults.OperatorImageTag, defaults.OperatorImagePullPolicy)
	}
	if defaults.AgentImageRepository != "hermes-agent" || defaults.AgentImageTag != "v2026.5.29.2" {
		t.Fatalf("default agent image = %q:%q, want hermes-agent:v2026.5.29.2",
			defaults.AgentImageRepository, defaults.AgentImageTag)
	}
	if defaults.HonchoImageRepository != "ghcr.io/plastic-labs/honcho" || defaults.HonchoImageTag != "0.1.0" {
		t.Fatalf("default Honcho image = %q:%q, want ghcr.io/plastic-labs/honcho:0.1.0",
			defaults.HonchoImageRepository, defaults.HonchoImageTag)
	}

	env := map[string]string{
		"HERMES_E2E_OPERATOR_NAMESPACE":         "hermes-op-run-1",
		"HERMES_E2E_WORKLOAD_NAMESPACE":         "hermes-work-run-1",
		"HERMES_E2E_MINIO_NAMESPACE":            "hermes-minio-run-1",
		"HERMES_E2E_OPERATOR_IMAGE_REPOSITORY":  "registry.example.com/hermes/operator",
		"HERMES_E2E_OPERATOR_IMAGE_TAG":         "sha-operator",
		"HERMES_E2E_OPERATOR_IMAGE_PULL_POLICY": "Always",
		"HERMES_E2E_AGENT_IMAGE_REPOSITORY":     "registry.example.com/hermes/agent",
		"HERMES_E2E_AGENT_IMAGE_TAG":            "sha-agent",
		"HERMES_E2E_HONCHO_IMAGE_REPOSITORY":    "registry.example.com/hermes/honcho",
		"HERMES_E2E_HONCHO_IMAGE_TAG":           "sha-honcho",
	}
	overrides := newE2EConfigFromEnv(func(key string) string { return env[key] })
	if overrides.OperatorNamespace != "hermes-op-run-1" {
		t.Fatalf("operator namespace override = %q", overrides.OperatorNamespace)
	}
	if overrides.WorkloadNamespace != "hermes-work-run-1" {
		t.Fatalf("workload namespace override = %q", overrides.WorkloadNamespace)
	}
	if overrides.MinIONamespace != "hermes-minio-run-1" {
		t.Fatalf("MinIO namespace override = %q", overrides.MinIONamespace)
	}
	if overrides.OperatorImageRepository != "registry.example.com/hermes/operator" ||
		overrides.OperatorImageTag != "sha-operator" ||
		overrides.OperatorImagePullPolicy != "Always" {
		t.Fatalf("operator image overrides not applied: %#v", overrides)
	}
	if overrides.AgentImageRepository != "registry.example.com/hermes/agent" || overrides.AgentImageTag != "sha-agent" {
		t.Fatalf("agent image overrides not applied: %#v", overrides)
	}
	if overrides.HonchoImageRepository != "registry.example.com/hermes/honcho" || overrides.HonchoImageTag != "sha-honcho" {
		t.Fatalf("Honcho image overrides not applied: %#v", overrides)
	}
}

func TestRenderE2ETemplateSubstitutesRuntimeConfig(t *testing.T) {
	cfg := e2eConfig{
		WorkloadNamespace:     "hermes-work-run-2",
		MinIONamespace:        "hermes-minio-run-2",
		AgentImageRepository:  "registry.example.com/hermes/agent",
		AgentImageTag:         "sha-agent",
		HonchoImageRepository: "registry.example.com/hermes/honcho",
		HonchoImageTag:        "sha-honcho",
	}

	rendered, err := renderE2ETemplate(`metadata:
  namespace: {{ .WorkloadNamespace }}
spec:
  image:
    repository: {{ .AgentImageRepository }}
    tag: {{ .AgentImageTag }}
  backup:
    s3:
      endpoint: {{ .MinIOEndpoint }}
  profileStore:
    honcho:
      image:
        repository: {{ .HonchoImageRepository }}
        tag: {{ .HonchoImageTag }}
`, cfg)
	if err != nil {
		t.Fatalf("render template: %v", err)
	}
	for _, want := range []string{
		"namespace: hermes-work-run-2",
		"repository: registry.example.com/hermes/agent",
		"tag: sha-agent",
		"endpoint: http://minio.hermes-minio-run-2.svc:9000",
		"repository: registry.example.com/hermes/honcho",
		"tag: sha-honcho",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered manifest missing %q:\n%s", want, rendered)
		}
	}
	for _, forbidden := range []string{"namespace: default", "minio.minio.svc"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered manifest retained hard-coded %q:\n%s", forbidden, rendered)
		}
	}
}

func TestE2ENetworkPoliciesScopeWorkloadMinIOAndOperator(t *testing.T) {
	cfg := e2eConfig{
		OperatorNamespace: "hermes-op-run-3",
		WorkloadNamespace: "hermes-work-run-3",
		MinIONamespace:    "hermes-minio-run-3",
	}
	policies := parseNetworkPolicies(t, e2eNetworkPolicyManifest(cfg))

	workloadDeny := policies["hermes-e2e-workload-default-deny"]
	if workloadDeny.Namespace != cfg.WorkloadNamespace {
		t.Fatalf("workload default deny namespace = %q", workloadDeny.Namespace)
	}
	if len(workloadDeny.Spec.PodSelector.MatchLabels) != 0 {
		t.Fatalf("workload default deny should select all pods, got %#v", workloadDeny.Spec.PodSelector.MatchLabels)
	}
	if !hasPolicyTypes(workloadDeny, networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress) {
		t.Fatalf("workload default deny policyTypes = %#v", workloadDeny.Spec.PolicyTypes)
	}

	workloadDNS := policies["hermes-e2e-workload-dns-egress"]
	if !allowsNamespacePort(workloadDNS.Spec.Egress, "kube-system", 53, corev1.ProtocolUDP) ||
		!allowsNamespacePort(workloadDNS.Spec.Egress, "kube-system", 53, corev1.ProtocolTCP) {
		t.Fatalf("workload DNS policy does not allow UDP/TCP 53 to kube-system: %#v", workloadDNS.Spec.Egress)
	}

	workloadMinIO := policies["hermes-e2e-workload-minio-egress"]
	if !allowsNamespacePodPort(workloadMinIO.Spec.Egress, cfg.MinIONamespace, "app", "minio", 9000, corev1.ProtocolTCP) {
		t.Fatalf("workload MinIO policy does not allow egress to MinIO 9000: %#v", workloadMinIO.Spec.Egress)
	}

	minioDeny := policies["hermes-e2e-minio-default-deny"]
	if minioDeny.Namespace != cfg.MinIONamespace {
		t.Fatalf("MinIO default deny namespace = %q", minioDeny.Namespace)
	}
	if !hasPolicyTypes(minioDeny, networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress) {
		t.Fatalf("MinIO default deny policyTypes = %#v", minioDeny.Spec.PolicyTypes)
	}

	minioIngress := policies["hermes-e2e-minio-ingress-from-workload"]
	if !allowsIngressFromNamespacePort(minioIngress.Spec.Ingress, cfg.WorkloadNamespace, 9000, corev1.ProtocolTCP) {
		t.Fatalf("MinIO ingress policy does not allow workload namespace on 9000: %#v", minioIngress.Spec.Ingress)
	}

	operatorWebhook := policies["hermes-e2e-operator-webhook-ingress"]
	if operatorWebhook.Namespace != cfg.OperatorNamespace {
		t.Fatalf("operator policy namespace = %q", operatorWebhook.Namespace)
	}
	if hasPolicyTypes(operatorWebhook, networkingv1.PolicyTypeEgress) {
		t.Fatalf("operator policy must not restrict egress to the API server: %#v", operatorWebhook.Spec.PolicyTypes)
	}
	if !allowsIngressPort(operatorWebhook.Spec.Ingress, 9443, corev1.ProtocolTCP) {
		t.Fatalf("operator policy does not allow webhook ingress on 9443: %#v", operatorWebhook.Spec.Ingress)
	}
	if !allowsIngressPort(operatorWebhook.Spec.Ingress, 8081, corev1.ProtocolTCP) {
		t.Fatalf("operator policy does not allow health probe ingress on 8081: %#v", operatorWebhook.Spec.Ingress)
	}
	if !allowsIngressPort(operatorWebhook.Spec.Ingress, 8443, corev1.ProtocolTCP) {
		t.Fatalf("operator policy does not allow metrics ingress on 8443: %#v", operatorWebhook.Spec.Ingress)
	}
}

func TestE2ECleanupCommandsUseConfiguredNamespaces(t *testing.T) {
	cfg := e2eConfig{
		OperatorNamespace: "hermes-op-run-4",
		WorkloadNamespace: "hermes-work-run-4",
		MinIONamespace:    "hermes-minio-run-4",
	}
	commands := e2eCleanupCommands(cfg)
	joined := strings.Join(flattenCommands(commands), "\n")

	for _, want := range []string{
		"helm uninstall hermes-operator --namespace hermes-op-run-4",
		"kubectl delete networkpolicy hermes-e2e-workload-default-deny hermes-e2e-workload-dns-egress hermes-e2e-workload-minio-egress -n hermes-work-run-4",
		"kubectl delete networkpolicy hermes-e2e-minio-default-deny hermes-e2e-minio-ingress-from-workload -n hermes-minio-run-4",
		"kubectl delete networkpolicy hermes-e2e-operator-webhook-ingress -n hermes-op-run-4",
		"kubectl delete hermesinstance e2e-demo e2e-br e2e-restore e2e-gateways hermes-from-oc -n hermes-work-run-4",
		"kubectl delete namespace hermes-work-run-4",
		"kubectl delete namespace hermes-minio-run-4",
		"kubectl delete namespace hermes-op-run-4",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("cleanup commands missing %q:\n%s", want, joined)
		}
	}
	for _, forbidden := range []string{" -n default", "--namespace hermes-system", " namespace minio"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("cleanup commands retained hard-coded namespace %q:\n%s", forbidden, joined)
		}
	}
}

func parseNetworkPolicies(t *testing.T, manifest string) map[string]networkingv1.NetworkPolicy {
	t.Helper()

	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader([]byte(manifest))))
	policies := make(map[string]networkingv1.NetworkPolicy)
	for {
		doc, err := reader.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("read yaml doc: %v", err)
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		var policy networkingv1.NetworkPolicy
		if err := yaml.Unmarshal(doc, &policy); err != nil {
			t.Fatalf("unmarshal network policy: %v\n%s", err, string(doc))
		}
		if policy.Kind != "NetworkPolicy" {
			continue
		}
		policies[policy.Name] = policy
	}
	return policies
}

func hasPolicyTypes(policy networkingv1.NetworkPolicy, want ...networkingv1.PolicyType) bool {
	seen := make(map[networkingv1.PolicyType]bool, len(policy.Spec.PolicyTypes))
	for _, policyType := range policy.Spec.PolicyTypes {
		seen[policyType] = true
	}
	for _, policyType := range want {
		if !seen[policyType] {
			return false
		}
	}
	return true
}

func allowsNamespacePort(rules []networkingv1.NetworkPolicyEgressRule, namespace string, port int, protocol corev1.Protocol) bool {
	for _, rule := range rules {
		if !ruleHasPort(rule.Ports, port, protocol) {
			continue
		}
		for _, peer := range rule.To {
			if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == namespace && peer.PodSelector == nil {
				return true
			}
		}
	}
	return false
}

func allowsNamespacePodPort(rules []networkingv1.NetworkPolicyEgressRule, namespace, labelKey, labelValue string, port int, protocol corev1.Protocol) bool {
	for _, rule := range rules {
		if !ruleHasPort(rule.Ports, port, protocol) {
			continue
		}
		for _, peer := range rule.To {
			if peer.NamespaceSelector == nil || peer.PodSelector == nil {
				continue
			}
			if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == namespace &&
				peer.PodSelector.MatchLabels[labelKey] == labelValue {
				return true
			}
		}
	}
	return false
}

func allowsIngressFromNamespacePort(rules []networkingv1.NetworkPolicyIngressRule, namespace string, port int, protocol corev1.Protocol) bool {
	for _, rule := range rules {
		if !ruleHasPort(rule.Ports, port, protocol) {
			continue
		}
		for _, peer := range rule.From {
			if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == namespace && peer.PodSelector == nil {
				return true
			}
		}
	}
	return false
}

func allowsIngressPort(rules []networkingv1.NetworkPolicyIngressRule, port int, protocol corev1.Protocol) bool {
	for _, rule := range rules {
		if ruleHasPort(rule.Ports, port, protocol) {
			return true
		}
	}
	return false
}

func ruleHasPort(ports []networkingv1.NetworkPolicyPort, port int, protocol corev1.Protocol) bool {
	for _, candidate := range ports {
		if candidate.Protocol == nil || *candidate.Protocol != protocol {
			continue
		}
		if candidate.Port != nil && candidate.Port.IntValue() == port {
			return true
		}
	}
	return false
}

func flattenCommands(commands []commandSpec) []string {
	flattened := make([]string, 0, len(commands))
	for _, command := range commands {
		flattened = append(flattened, command.name+" "+strings.Join(command.args, " "))
	}
	return flattened
}
