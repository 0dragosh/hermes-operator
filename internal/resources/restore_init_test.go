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

func restoreInstance() *hermesv1.HermesInstance {
	return &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image: hermesv1.ImageSpec{
				Repository: "ghcr.io/paperclipinc/hermes-agent",
				Tag:        "1.2.3",
			},
			RestoreFrom: "prod/agents/demo/2026-05-10T03-00-00Z.tar.zst",
			Backup: hermesv1.BackupSpec{
				S3: &hermesv1.BackupS3Spec{
					Bucket:               "hermes-backups",
					Endpoint:             "s3.amazonaws.com",
					Region:               "us-east-1",
					CredentialsSecretRef: hermesv1.LocalObjectReference{Name: "hermes-s3-creds"},
				},
			},
		},
	}
}

func TestBuildRestoreInitContainer_NameAndImage(t *testing.T) {
	c := BuildRestoreInitContainer(restoreInstance())
	require.NotNil(t, c)
	assert.Equal(t, "init-restore", c.Name)
	assert.Equal(t, "ghcr.io/paperclipinc/hermes-agent:1.2.3", c.Image)
}

func TestBuildRestoreInitContainer_HonorsBackupImageOverride(t *testing.T) {
	inst := restoreInstance()
	inst.Spec.Backup.Image = "internal.registry/hermes-backup-tools:9.9.9"
	c := BuildRestoreInitContainer(inst)
	require.NotNil(t, c)
	assert.Equal(t, "internal.registry/hermes-backup-tools:9.9.9", c.Image)
}

func TestBuildRestoreInitContainer_UsesRawS3RestoreCommand(t *testing.T) {
	c := BuildRestoreInitContainer(restoreInstance())
	joined := strings.Join(c.Args, " ")
	assert.Contains(t, joined, `aws --endpoint-url "$S3_ENDPOINT_URL" s3 cp "s3://${S3_BUCKET}/${SNAPSHOT_KEY}" - --no-progress`)
	assert.Contains(t, joined, "zstd -d")
	assert.Contains(t, joined, `tar -xf - -C "$DEST"`)
	assert.NotContains(t, joined, "restic")
}

func TestBuildRestoreInitContainer_CommandDoesNotEmbedRawS3Values(t *testing.T) {
	c := BuildRestoreInitContainer(restoreInstance())
	joined := strings.Join(c.Args, " ")
	assert.NotContains(t, joined, "hermes-backups")
	assert.NotContains(t, joined, "prod/agents/demo/2026-05-10T03-00-00Z.tar.zst")
	assert.NotContains(t, joined, "s3.amazonaws.com")
}

func TestBuildRestoreInitContainer_EmptyDestinationGuardRemains(t *testing.T) {
	c := BuildRestoreInitContainer(restoreInstance())
	joined := strings.Join(c.Args, " ")
	assert.Contains(t, joined, `DEST=/home/hermes/.hermes`)
	assert.Contains(t, joined, `[ -n "$(ls -A "$DEST" 2>/dev/null)" ]`)
	assert.Contains(t, joined, `HERMES_RESTORE_FORCE`)
	assert.Contains(t, joined, "refusing to overwrite")
}

func TestBuildRestoreInitContainer_SecurityContext(t *testing.T) {
	c := BuildRestoreInitContainer(restoreInstance())
	require.NotNil(t, c.SecurityContext)
	require.NotNil(t, c.SecurityContext.AllowPrivilegeEscalation)
	assert.False(t, *c.SecurityContext.AllowPrivilegeEscalation)
	require.NotNil(t, c.SecurityContext.ReadOnlyRootFilesystem)
	assert.True(t, *c.SecurityContext.ReadOnlyRootFilesystem)
}

func TestBuildRestoreInitContainer_S3ValuesAndCredsUseExplicitEnv(t *testing.T) {
	c := BuildRestoreInitContainer(restoreInstance())
	assert.Empty(t, c.EnvFrom)

	env := containerEnvByName(c)
	assert.Equal(t, "hermes-backups", env["S3_BUCKET"].Value)
	assert.Equal(t, "prod/agents/demo/2026-05-10T03-00-00Z.tar.zst", env["SNAPSHOT_KEY"].Value)
	assert.Equal(t, "https://s3.amazonaws.com", env["S3_ENDPOINT_URL"].Value)
	assert.Equal(t, "us-east-1", env["AWS_DEFAULT_REGION"].Value)
	assertSecretKeyRef(t, env["AWS_ACCESS_KEY_ID"], "hermes-s3-creds", "S3_ACCESS_KEY_ID")
	assertSecretKeyRef(t, env["AWS_SECRET_ACCESS_KEY"], "hermes-s3-creds", "S3_SECRET_ACCESS_KEY")
}

func TestBuildRestoreInitContainer_VolumeMountsDataAndTmp(t *testing.T) {
	c := BuildRestoreInitContainer(restoreInstance())
	mounts := map[string]string{}
	for _, mount := range c.VolumeMounts {
		mounts[mount.Name] = mount.MountPath
	}
	assert.Equal(t, "/home/hermes/.hermes", mounts["data"])
	assert.Equal(t, "/tmp", mounts["tmp"])
}

func TestBuildRestoreInitContainer_NilWhenNoRestore(t *testing.T) {
	inst := restoreInstance()
	inst.Spec.RestoreFrom = ""
	assert.Nil(t, BuildRestoreInitContainer(inst))
}

func TestBuildRestoreInitContainer_NilWhenAlreadyRestored(t *testing.T) {
	inst := restoreInstance()
	inst.Status.RestoredFrom = inst.Spec.RestoreFrom
	assert.Nil(t, BuildRestoreInitContainer(inst))
}

func containerEnvByName(c *corev1.Container) map[string]corev1.EnvVar {
	out := map[string]corev1.EnvVar{}
	for _, env := range c.Env {
		out[env.Name] = env
	}
	return out
}

func assertSecretKeyRef(t *testing.T, env corev1.EnvVar, secretName, key string) {
	t.Helper()
	require.NotNil(t, env.ValueFrom)
	require.NotNil(t, env.ValueFrom.SecretKeyRef)
	assert.Equal(t, secretName, env.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, key, env.ValueFrom.SecretKeyRef.Key)
}
