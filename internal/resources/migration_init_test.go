package resources

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func migrationInstanceWithOpenClawRef() *hermesv1.HermesInstance {
	return &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image: hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent", Tag: "1.0.0"},
			Migration: hermesv1.MigrationSpec{
				FromOpenClaw: &hermesv1.MigrationFromOpenClawSpec{
					Mode: "copy",
					Source: hermesv1.MigrationFromOpenClawSource{
						OpenClawInstanceRef: &hermesv1.NamespacedObjectReference{
							Name:      "my-openclaw",
							Namespace: "agents",
						},
					},
				},
			},
		},
	}
}

func migrationInstanceWithS3() *hermesv1.HermesInstance {
	return &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image: hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent", Tag: "1.0.0"},
			Migration: hermesv1.MigrationSpec{
				FromOpenClaw: &hermesv1.MigrationFromOpenClawSpec{
					Mode: "copy",
					Source: hermesv1.MigrationFromOpenClawSource{
						BackupRef: &hermesv1.MigrationBackupRef{
							S3: hermesv1.MigrationBackupS3{
								Bucket:               "openclaw-backups",
								Endpoint:             "s3.amazonaws.com",
								Region:               "us-east-1",
								Key:                  "prod/my-openclaw/2026-05-11.tar.zst",
								CredentialsSecretRef: hermesv1.LocalObjectReference{Name: "oc-s3-creds"},
							},
						},
					},
				},
			},
		},
	}
}

func TestBuildMigrationInitContainer_NilWhenNoSpec(t *testing.T) {
	inst := &hermesv1.HermesInstance{}
	assert.Nil(t, BuildMigrationInitContainer(inst))
}

func TestBuildMigrationInitContainer_NilWhenCompleted(t *testing.T) {
	inst := migrationInstanceWithOpenClawRef()
	inst.Status.Migration.Completed = true
	assert.Nil(t, BuildMigrationInitContainer(inst))
}

func TestBuildMigrationInitContainer_OpenClawRef_Name(t *testing.T) {
	c := BuildMigrationInitContainer(migrationInstanceWithOpenClawRef())
	require.NotNil(t, c)
	assert.Equal(t, "init-migrate-from-openclaw", c.Name)
	assert.Equal(t, "ghcr.io/paperclipinc/hermes-agent:1.0.0", c.Image)
}

func TestBuildMigrationInitContainer_DefaultImageUsesInstanceAgentImageBehavior(t *testing.T) {
	inst := migrationInstanceWithOpenClawRef()
	inst.Spec.AutoUpdate.Enabled = true
	inst.Status.AutoUpdate.TargetTag = "2.0.0"
	c := BuildMigrationInitContainer(inst)
	require.NotNil(t, c)
	assert.Equal(t, "ghcr.io/paperclipinc/hermes-agent:2.0.0", c.Image)
}

func TestBuildMigrationInitContainer_OpenClawRef_Args(t *testing.T) {
	c := BuildMigrationInitContainer(migrationInstanceWithOpenClawRef())
	joined := strings.Join(c.Args, " ")
	assert.Contains(t, joined, "hermes-agent migrate from-openclaw")
	assert.Contains(t, joined, "--source /mnt/openclaw")
	assert.Contains(t, joined, "--dest /home/hermes/.hermes")
}

func TestBuildMigrationInitContainer_OpenClawRef_VolumeMount(t *testing.T) {
	c := BuildMigrationInitContainer(migrationInstanceWithOpenClawRef())
	found := map[string]string{}
	for _, m := range c.VolumeMounts {
		found[m.Name] = m.MountPath
	}
	assert.Equal(t, "/mnt/openclaw", found["openclaw-source"])
	assert.Equal(t, "/home/hermes/.hermes", found["data"])
}

func TestBuildMigrationInitContainer_S3_DownloadsBeforeMigrate(t *testing.T) {
	c := BuildMigrationInitContainer(migrationInstanceWithS3())
	joined := strings.Join(c.Args, " ")
	assert.Contains(t, joined, `aws --endpoint-url "$OPENCLAW_S3_ENDPOINT_URL"`)
	assert.Contains(t, joined, `"s3://${OPENCLAW_S3_BUCKET}/${OPENCLAW_S3_KEY}"`)
	assert.Contains(t, joined, "/mnt/openclaw")
	assert.Contains(t, joined, "hermes-agent migrate from-openclaw")
	assert.NotContains(t, joined, "restic")
	assert.NotContains(t, joined, "openclaw-backups")
	assert.NotContains(t, joined, "prod/my-openclaw/2026-05-11.tar.zst")
	assert.NotContains(t, joined, "s3.amazonaws.com")
}

func TestBuildMigrationInitContainer_S3_ExplicitCredentialKeyRefs(t *testing.T) {
	c := BuildMigrationInitContainer(migrationInstanceWithS3())
	assert.Empty(t, c.EnvFrom)

	env := containerEnvByName(c)
	assertSecretKeyRef(t, env["AWS_ACCESS_KEY_ID"], "oc-s3-creds", "S3_ACCESS_KEY_ID")
	assertSecretKeyRef(t, env["AWS_SECRET_ACCESS_KEY"], "oc-s3-creds", "S3_SECRET_ACCESS_KEY")
}

func TestBuildMigrationInitContainer_S3_ValuesOnlyInEnv(t *testing.T) {
	c := BuildMigrationInitContainer(migrationInstanceWithS3())
	env := containerEnvByName(c)
	assert.Equal(t, "openclaw-backups", env["OPENCLAW_S3_BUCKET"].Value)
	assert.Equal(t, "prod/my-openclaw/2026-05-11.tar.zst", env["OPENCLAW_S3_KEY"].Value)
	assert.Equal(t, "https://s3.amazonaws.com", env["OPENCLAW_S3_ENDPOINT_URL"].Value)
	assert.Equal(t, "us-east-1", env["AWS_DEFAULT_REGION"].Value)
}

func TestBuildMigrationInitContainer_VolumeMountsWritableTmp(t *testing.T) {
	cases := map[string]*hermesv1.HermesInstance{
		"openclaw ref": migrationInstanceWithOpenClawRef(),
		"s3 backup":    migrationInstanceWithS3(),
	}
	for name, inst := range cases {
		t.Run(name, func(t *testing.T) {
			c := BuildMigrationInitContainer(inst)
			require.NotNil(t, c)
			found := map[string]string{}
			for _, m := range c.VolumeMounts {
				found[m.Name] = m.MountPath
			}
			assert.Equal(t, "/tmp", found["tmp"])
		})
	}
}

func TestBuildMigrationInitContainer_CustomImage(t *testing.T) {
	inst := migrationInstanceWithOpenClawRef()
	inst.Spec.Migration.FromOpenClaw.Image = "internal.registry/hermes-agent:migrate"
	c := BuildMigrationInitContainer(inst)
	assert.Equal(t, "internal.registry/hermes-agent:migrate", c.Image)
}
