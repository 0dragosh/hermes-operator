package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup → delete → restore cycle (MinIO)", func() {
	It("performs a full backup, on-delete final backup, and restore", func() {
		cfg := e2eConfigFromEnv()
		ns := cfg.WorkloadNamespace

		manifest, err := renderE2ETemplate(`
apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: e2e-br
  namespace: {{ .WorkloadNamespace }}
spec:
  image:
    repository: {{ .AgentImageRepository }}
    tag: "{{ .AgentImageTag }}"
    pullPolicy: IfNotPresent
  storage:
    persistence:
      enabled: true
      size: 1Gi
  backup:
    onDelete: true
    schedule: "*/2 * * * *"
    s3:
      bucket: hermes-backups
      endpoint: {{ .MinIOEndpoint }}
      region: us-east-1
      pathPrefix: e2e/
      credentialsSecretRef:
        name: hermes-s3-creds
`, cfg)
		Expect(err).NotTo(HaveOccurred(), "render backup manifest")

		out, err := runStdin("kubectl", []string{"apply", "-f", "-"}, manifest)
		Expect(err).ToNot(HaveOccurred(), "kubectl apply: %s", out)

		Eventually(func() string {
			out, _ := kubectl("get", "cronjob/e2e-br-backup-cron", "-n", ns, "-o", "jsonpath={.metadata.name}")
			return strings.TrimSpace(out)
		}, 2*time.Minute).Should(Equal("e2e-br-backup-cron"))

		out, err = kubectl("create", "job", "manual-1", "-n", ns, "--from=cronjob/e2e-br-backup-cron")
		Expect(err).ToNot(HaveOccurred(), "create manual job: %s", out)

		Eventually(func() string {
			out, _ := kubectl("get", "job/manual-1", "-n", ns, "-o", "jsonpath={.status.succeeded}")
			return strings.TrimSpace(out)
		}, 3*time.Minute).Should(Equal("1"))

		var snapshotKey string
		Eventually(func() string {
			snapshotKey = findFirstSnapshotKey(ns, "e2e-br")
			return snapshotKey
		}, time.Minute, 2*time.Second).ShouldNot(BeEmpty(), "expected at least one snapshot in the bucket")
		Expect(snapshotKey).To(HavePrefix("e2e/" + ns + "/e2e-br/"))
		Expect(snapshotKey).To(HaveSuffix(".tar.zst"))
		GinkgoWriter.Printf("found scheduled snapshot: %s\n", snapshotKey)
		expectNoResticRepositoryLayout()

		out, err = kubectl("delete", "hermesinstance/e2e-br", "-n", ns, "--wait=false")
		Expect(err).ToNot(HaveOccurred(), "delete: %s", out)

		Eventually(func() bool {
			out, _ := kubectl("get", "hermesinstance/e2e-br", "-n", ns, "--ignore-not-found")
			return strings.TrimSpace(out) == ""
		}, 5*time.Minute).Should(BeTrue())

		restoreManifest, err := renderE2ETemplate(fmt.Sprintf(`
apiVersion: hermes.agent/v1
kind: HermesInstance
metadata:
  name: e2e-restore
  namespace: {{ .WorkloadNamespace }}
spec:
  image:
    repository: {{ .AgentImageRepository }}
    tag: "{{ .AgentImageTag }}"
    pullPolicy: IfNotPresent
  storage:
    persistence:
      enabled: true
      size: 1Gi
  restoreFrom: %q
  backup:
    s3:
      bucket: hermes-backups
      endpoint: {{ .MinIOEndpoint }}
      region: us-east-1
      pathPrefix: e2e/
      credentialsSecretRef:
        name: hermes-s3-creds
`, snapshotKey), cfg)
		Expect(err).NotTo(HaveOccurred(), "render restore manifest")

		out, err = runStdin("kubectl", []string{"apply", "-f", "-"}, restoreManifest)
		Expect(err).ToNot(HaveOccurred(), "apply restore: %s", out)

		Eventually(func() string {
			out, _ := kubectl("get", "hermesinstance/e2e-restore", "-n", ns, "-o", "jsonpath={.status.restoredFrom}")
			return strings.TrimSpace(out)
		}, 5*time.Minute).Should(Equal(snapshotKey))

		_, _ = kubectl("delete", "hermesinstance/e2e-restore", "-n", ns, "--ignore-not-found")
	})
})

// findFirstSnapshotKey lists the bucket prefix and returns the first key, or "" if nothing.
func findFirstSnapshotKey(namespace, instance string) string {
	prefix := "e2e/" + namespace + "/" + instance + "/"
	keys, err := listHermesBackupKeys(prefix)
	Expect(err).NotTo(HaveOccurred(), "list backup keys for prefix %q", prefix)
	for _, key := range keys {
		if strings.HasSuffix(key, ".tar.zst") {
			return key
		}
	}
	return ""
}

func expectNoResticRepositoryLayout() {
	keys, err := listHermesBackupKeys("")
	Expect(err).NotTo(HaveOccurred(), "list backup keys for repository layout check")
	for _, disallowed := range []string{"config", "data/", "index/", "snapshots/"} {
		for _, key := range keys {
			Expect(key).NotTo(HavePrefix(disallowed), "legacy repository layout should not be present in raw backup bucket")
		}
	}
}

func listHermesBackupKeys(prefix string) ([]string, error) {
	cfg := e2eConfigFromEnv()
	cmd := []string{
		"run", "mc-list", "--namespace", cfg.WorkloadNamespace, "--rm", "-i", "--restart=Never",
		"--image=minio/mc:RELEASE.2024-09-16T17-43-14Z",
		"--env=MINIO_ENDPOINT=" + cfg.MinIOEndpoint(),
		"--env=BACKUP_PREFIX=" + prefix,
		"--command", "--", "/bin/sh", "-c",
		`mc alias set local "$MINIO_ENDPOINT" minioadmin minioadmin >/dev/null 2>&1 && mc ls --recursive "local/hermes-backups/${BACKUP_PREFIX}"`,
	}
	out, err := kubectl(cmd...)
	if err != nil {
		return nil, fmt.Errorf("kubectl %s failed: %w\n%s", strings.Join(cmd, " "), err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	keys := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		key := fields[len(fields)-1]
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			key = prefix + strings.TrimLeft(key, "/")
		}
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys, nil
}
