package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

const (
	defaultOperatorNamespace       = "hermes-system"
	defaultWorkloadNamespace       = "default"
	defaultMinIONamespace          = "minio"
	defaultOperatorImageRepository = "hermes-operator"
	defaultOperatorImageTag        = "dev"
	defaultOperatorImagePullPolicy = "IfNotPresent"
	defaultAgentImageRepository    = "hermes-agent"
	defaultAgentImageTag           = "v2026.5.29.2"
	defaultHonchoImageRepository   = "ghcr.io/plastic-labs/honcho"
	defaultHonchoImageTag          = "0.1.0"
)

type e2eConfig struct {
	OperatorNamespace       string
	WorkloadNamespace       string
	MinIONamespace          string
	OperatorImageRepository string
	OperatorImageTag        string
	OperatorImagePullPolicy string
	AgentImageRepository    string
	AgentImageTag           string
	HonchoImageRepository   string
	HonchoImageTag          string
}

type commandSpec struct {
	name string
	args []string
}

func e2eConfigFromEnv() e2eConfig {
	return newE2EConfigFromEnv(os.Getenv)
}

func newE2EConfigFromEnv(getenv func(string) string) e2eConfig {
	return e2eConfig{
		OperatorNamespace:       envOrDefault(getenv, "HERMES_E2E_OPERATOR_NAMESPACE", defaultOperatorNamespace),
		WorkloadNamespace:       envOrDefault(getenv, "HERMES_E2E_WORKLOAD_NAMESPACE", defaultWorkloadNamespace),
		MinIONamespace:          envOrDefault(getenv, "HERMES_E2E_MINIO_NAMESPACE", defaultMinIONamespace),
		OperatorImageRepository: envOrDefault(getenv, "HERMES_E2E_OPERATOR_IMAGE_REPOSITORY", defaultOperatorImageRepository),
		OperatorImageTag:        envOrDefault(getenv, "HERMES_E2E_OPERATOR_IMAGE_TAG", defaultOperatorImageTag),
		OperatorImagePullPolicy: envOrDefault(getenv, "HERMES_E2E_OPERATOR_IMAGE_PULL_POLICY", defaultOperatorImagePullPolicy),
		AgentImageRepository:    envOrDefault(getenv, "HERMES_E2E_AGENT_IMAGE_REPOSITORY", defaultAgentImageRepository),
		AgentImageTag:           envOrDefault(getenv, "HERMES_E2E_AGENT_IMAGE_TAG", defaultAgentImageTag),
		HonchoImageRepository:   envOrDefault(getenv, "HERMES_E2E_HONCHO_IMAGE_REPOSITORY", defaultHonchoImageRepository),
		HonchoImageTag:          envOrDefault(getenv, "HERMES_E2E_HONCHO_IMAGE_TAG", defaultHonchoImageTag),
	}
}

func envOrDefault(getenv func(string) string, key, defaultValue string) string {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return defaultValue
	}
	return value
}

func (c e2eConfig) MinIOEndpoint() string {
	return fmt.Sprintf("http://minio.%s.svc:9000", c.MinIONamespace)
}

func renderE2ETemplate(body string, cfg e2eConfig) (string, error) {
	tmpl, err := template.New("e2e").Option("missingkey=error").Parse(body)
	if err != nil {
		return "", err
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, cfg); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

func renderE2ETemplateFile(path string, cfg e2eConfig) (string, error) {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return renderE2ETemplate(string(body), cfg)
}

func e2eNetworkPolicyManifest(cfg e2eConfig) string {
	// The operator policy intentionally does not restrict egress. The manager
	// must reach the Kubernetes API, and the API server's source/destination is
	// not expressible portably with NetworkPolicy across kind and managed
	// clusters. Ingress is limited to the operator's exposed webhook, health,
	// and metrics ports while leaving the source open so API-server admission
	// calls and kubelet probes continue to work.
	return fmt.Sprintf(`
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: hermes-e2e-workload-default-deny
  namespace: %[1]s
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: hermes-e2e-workload-dns-egress
  namespace: %[1]s
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: hermes-e2e-workload-minio-egress
  namespace: %[1]s
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: %[2]s
          podSelector:
            matchLabels:
              app: minio
      ports:
        - protocol: TCP
          port: 9000
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: hermes-e2e-minio-default-deny
  namespace: %[2]s
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: hermes-e2e-minio-ingress-from-workload
  namespace: %[2]s
spec:
  podSelector:
    matchLabels:
      app: minio
  policyTypes: [Ingress]
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: %[1]s
      ports:
        - protocol: TCP
          port: 9000
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: hermes-e2e-operator-webhook-ingress
  namespace: %[3]s
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: hermes-operator
  policyTypes: [Ingress]
  ingress:
    - ports:
        - protocol: TCP
          port: 9443
        - protocol: TCP
          port: 8081
        - protocol: TCP
          port: 8443
`, cfg.WorkloadNamespace, cfg.MinIONamespace, cfg.OperatorNamespace)
}

func e2eCleanupCommands(cfg e2eConfig) []commandSpec {
	commands := []commandSpec{
		{
			name: "kubectl",
			args: []string{
				"delete", "hermesinstance",
				"e2e-demo", "e2e-br", "e2e-restore", "e2e-gateways", "hermes-from-oc",
				"-n", cfg.WorkloadNamespace,
				"--ignore-not-found=true", "--wait=false",
			},
		},
		{
			name: "kubectl",
			args: []string{
				"delete", "secret",
				"tg-secret", "honcho-secret", "hermes-s3-creds",
				"-n", cfg.WorkloadNamespace,
				"--ignore-not-found=true", "--wait=false",
			},
		},
		{
			name: "kubectl",
			args: []string{
				"delete", "job", "manual-1", "mc-list",
				"-n", cfg.WorkloadNamespace,
				"--ignore-not-found=true", "--wait=false",
			},
		},
		{
			name: "kubectl",
			args: []string{
				"delete", "deploy/minio", "svc/minio", "secret/minio-root", "job/mc-mkbucket",
				"-n", cfg.MinIONamespace,
				"--ignore-not-found=true", "--wait=false",
			},
		},
		{
			name: "kubectl",
			args: []string{
				"delete", "networkpolicy",
				"hermes-e2e-workload-default-deny", "hermes-e2e-workload-dns-egress", "hermes-e2e-workload-minio-egress",
				"-n", cfg.WorkloadNamespace,
				"--ignore-not-found=true", "--wait=false",
			},
		},
		{
			name: "kubectl",
			args: []string{
				"delete", "networkpolicy",
				"hermes-e2e-minio-default-deny", "hermes-e2e-minio-ingress-from-workload",
				"-n", cfg.MinIONamespace,
				"--ignore-not-found=true", "--wait=false",
			},
		},
		{
			name: "kubectl",
			args: []string{
				"delete", "networkpolicy",
				"hermes-e2e-operator-webhook-ingress",
				"-n", cfg.OperatorNamespace,
				"--ignore-not-found=true", "--wait=false",
			},
		},
		{
			name: "helm",
			args: []string{"uninstall", "hermes-operator", "--namespace", cfg.OperatorNamespace},
		},
	}

	for _, ns := range cleanupNamespaces(cfg) {
		commands = append(commands, commandSpec{
			name: "kubectl",
			args: []string{"delete", "namespace", ns, "--ignore-not-found=true", "--wait=false"},
		})
	}
	return commands
}

func cleanupNamespaces(cfg e2eConfig) []string {
	candidates := []string{cfg.WorkloadNamespace, cfg.MinIONamespace, cfg.OperatorNamespace}
	seen := make(map[string]bool, len(candidates))
	namespaces := make([]string, 0, len(candidates))
	for _, ns := range candidates {
		if seen[ns] || isProtectedE2ENamespace(ns) {
			continue
		}
		seen[ns] = true
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

func isProtectedE2ENamespace(ns string) bool {
	switch ns {
	case "", "default", "minio", "hermes-system", "kube-system", "kube-public", "kube-node-lease":
		return true
	default:
		return false
	}
}
