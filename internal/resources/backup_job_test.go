package resources

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func backupInstance() *hermesv1.HermesInstance {
	return &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents", UID: "uid-1"},
		Spec: hermesv1.HermesInstanceSpec{
			Image: hermesv1.ImageSpec{
				Repository: "ghcr.io/paperclipinc/hermes-agent",
				Tag:        "v1.2.3",
			},
			Backup: hermesv1.BackupSpec{
				S3: &hermesv1.BackupS3Spec{
					Bucket:               "hermes-backups",
					Endpoint:             "s3.amazonaws.com",
					Region:               "us-east-1",
					PathPrefix:           "prod/",
					CredentialsSecretRef: hermesv1.LocalObjectReference{Name: "hermes-s3-creds"},
				},
			},
		},
	}
}

func TestBuildBackupOneShotJob_DefaultsToAgentImageAndNames(t *testing.T) {
	inst := backupInstance()
	job := BuildBackupOneShotJob(inst, BackupJobOpts{
		Name:        "demo-backup-final",
		SnapshotKey: "prod/agents/demo/2026-05-10T03-00-00Z.tar.zst",
		Kind:        "onDelete",
	})
	assert.Equal(t, "demo-backup-final", job.Name)
	assert.Equal(t, "agents", job.Namespace)
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	c := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "ghcr.io/paperclipinc/hermes-agent:v1.2.3", c.Image)
	assert.Equal(t, corev1.PullIfNotPresent, c.ImagePullPolicy)
}

func TestBuildBackupOneShotJob_CustomImage(t *testing.T) {
	inst := backupInstance()
	inst.Spec.Backup.Image = "internal.registry/hermes-backup:custom"
	job := BuildBackupOneShotJob(inst, BackupJobOpts{Name: "demo-backup-final", SnapshotKey: "k", Kind: "onDelete"})
	assert.Equal(t, "internal.registry/hermes-backup:custom", job.Spec.Template.Spec.Containers[0].Image)
}

func TestBuildBackupOneShotJob_UsesRawS3EnvAndStaticScript(t *testing.T) {
	inst := backupInstance()
	snapshotKey := "prod/agents/demo/2026-05-10T03-00-00Z.tar.zst"
	job := BuildBackupOneShotJob(inst, BackupJobOpts{
		Name:        "demo-backup-final",
		SnapshotKey: snapshotKey,
		Kind:        "onDelete",
	})
	c := job.Spec.Template.Spec.Containers[0]
	cmd := containerCommandText(c)

	assertNoRestic(t, cmd)
	assert.NotContains(t, cmd, "hermes-backups")
	assert.NotContains(t, cmd, "s3.amazonaws.com")
	assert.NotContains(t, cmd, snapshotKey)
	assert.Contains(t, cmd, "mktemp -d /tmp/")
	assert.Contains(t, cmd, "meta.json")
	assert.Contains(t, cmd, "tar -C /home/hermes/.hermes")
	assert.Contains(t, cmd, "zstd")
	assert.Contains(t, cmd, `aws --endpoint-url "$S3_ENDPOINT_URL" s3 cp - "s3://$S3_BUCKET/$SNAPSHOT_KEY"`)
	assert.NotContains(t, cmd, "jq")

	assertEnvValue(t, c.Env, "S3_BUCKET", "hermes-backups")
	assertEnvValue(t, c.Env, "SNAPSHOT_KEY", snapshotKey)
	assertEnvValue(t, c.Env, "S3_ENDPOINT_URL", "https://s3.amazonaws.com")
	assertEnvValue(t, c.Env, "AWS_DEFAULT_REGION", "us-east-1")
}

func TestBuildBackupOneShotJob_PVCRef(t *testing.T) {
	inst := backupInstance()
	job := BuildBackupOneShotJob(inst, BackupJobOpts{Name: "demo-backup-final", SnapshotKey: "k", Kind: "onDelete"})
	volumes := job.Spec.Template.Spec.Volumes
	data := volumeByName(volumes, "data")
	require.NotNil(t, data)
	require.NotNil(t, data.PersistentVolumeClaim)
	assert.Equal(t, PVCName(inst), data.PersistentVolumeClaim.ClaimName)
	assert.True(t, data.PersistentVolumeClaim.ReadOnly)
	assert.True(t, job.Spec.Template.Spec.Containers[0].VolumeMounts[0].ReadOnly)
}

func TestBuildBackupOneShotJob_CoLocatesWithInstancePodForRWOPVC(t *testing.T) {
	inst := backupInstance()
	job := BuildBackupOneShotJob(inst, BackupJobOpts{Name: "demo-backup-final", SnapshotKey: "k", Kind: "onDelete"})
	affinity := job.Spec.Template.Spec.Affinity
	require.NotNil(t, affinity)
	require.NotNil(t, affinity.PodAffinity)
	require.Len(t, affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution, 1)

	term := affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0]
	assert.Equal(t, "kubernetes.io/hostname", term.TopologyKey)
	require.NotNil(t, term.LabelSelector)
	assert.Equal(t, map[string]string{
		"app.kubernetes.io/name":     "hermes-agent",
		"app.kubernetes.io/instance": "demo",
	}, term.LabelSelector.MatchLabels)
}

func TestBuildBackupOneShotJob_S3CredsViaExplicitSecretKeyRefs(t *testing.T) {
	inst := backupInstance()
	job := BuildBackupOneShotJob(inst, BackupJobOpts{Name: "demo-backup-final", SnapshotKey: "k", Kind: "onDelete"})
	c := job.Spec.Template.Spec.Containers[0]
	assert.Empty(t, c.EnvFrom)
	assertSecretKeyEnv(t, c.Env, "AWS_ACCESS_KEY_ID", "hermes-s3-creds", "S3_ACCESS_KEY_ID")
	assertSecretKeyEnv(t, c.Env, "AWS_SECRET_ACCESS_KEY", "hermes-s3-creds", "S3_SECRET_ACCESS_KEY")
}

func TestBuildBackupOneShotJob_TmpVolumeAndNoServiceAccountToken(t *testing.T) {
	inst := backupInstance()
	job := BuildBackupOneShotJob(inst, BackupJobOpts{Name: "demo-backup-final", SnapshotKey: "k", Kind: "onDelete"})
	pod := job.Spec.Template.Spec
	require.NotNil(t, pod.AutomountServiceAccountToken)
	assert.False(t, *pod.AutomountServiceAccountToken)
	assertTmpVolumeAndMount(t, pod.Volumes, pod.Containers[0].VolumeMounts)
}

func TestBuildBackupOneShotJob_BackoffAndTTL(t *testing.T) {
	inst := backupInstance()
	job := BuildBackupOneShotJob(inst, BackupJobOpts{Name: "demo-backup-final", SnapshotKey: "k", Kind: "onDelete"})
	require.NotNil(t, job.Spec.BackoffLimit)
	assert.Equal(t, int32(3), *job.Spec.BackoffLimit)
	require.NotNil(t, job.Spec.TTLSecondsAfterFinished)
	assert.Equal(t, int32(86400), *job.Spec.TTLSecondsAfterFinished)
}

func containerCommandText(c corev1.Container) string {
	return strings.Join(append(append([]string{}, c.Command...), c.Args...), " ")
}

func assertNoRestic(t *testing.T, text string) {
	t.Helper()
	assert.NotContains(t, strings.ToLower(text), "restic")
}

func assertEnvValue(t *testing.T, env []corev1.EnvVar, name, value string) {
	t.Helper()
	found := envByName(env, name)
	require.NotNil(t, found, "env %s", name)
	assert.Equal(t, value, found.Value)
	assert.Nil(t, found.ValueFrom)
}

func assertSecretKeyEnv(t *testing.T, env []corev1.EnvVar, name, secretName, key string) {
	t.Helper()
	found := envByName(env, name)
	require.NotNil(t, found, "env %s", name)
	require.NotNil(t, found.ValueFrom)
	require.NotNil(t, found.ValueFrom.SecretKeyRef)
	assert.Equal(t, secretName, found.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, key, found.ValueFrom.SecretKeyRef.Key)
}

func envByName(env []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}
	return nil
}

func assertTmpVolumeAndMount(t *testing.T, volumes []corev1.Volume, mounts []corev1.VolumeMount) {
	t.Helper()
	tmp := volumeByName(volumes, "tmp")
	require.NotNil(t, tmp)
	require.NotNil(t, tmp.EmptyDir)

	for _, mount := range mounts {
		if mount.Name == "tmp" {
			assert.Equal(t, "/tmp", mount.MountPath)
			return
		}
	}
	t.Fatalf("tmp volume mount not found")
}
