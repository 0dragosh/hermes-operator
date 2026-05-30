package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func cronInstance() *hermesv1.HermesInstance {
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
				Schedule: "0 3 * * *",
			},
		},
	}
}

func TestBuildBackupCronJob_BasicShape(t *testing.T) {
	cj := BuildBackupCronJob(cronInstance())
	require.NotNil(t, cj)
	assert.Equal(t, "demo-backup-cron", cj.Name)
	assert.Equal(t, "agents", cj.Namespace)
	assert.Equal(t, "0 3 * * *", cj.Spec.Schedule)
	assert.Equal(t, batchv1.ForbidConcurrent, cj.Spec.ConcurrencyPolicy)
}

func TestBuildBackupCronJob_HistoryLimitsFromSpec(t *testing.T) {
	inst := cronInstance()
	h := int32(7)
	f := int32(2)
	inst.Spec.Backup.HistoryLimit = &h
	inst.Spec.Backup.FailedHistoryLimit = &f
	cj := BuildBackupCronJob(inst)
	require.NotNil(t, cj.Spec.SuccessfulJobsHistoryLimit)
	require.NotNil(t, cj.Spec.FailedJobsHistoryLimit)
	assert.Equal(t, int32(7), *cj.Spec.SuccessfulJobsHistoryLimit)
	assert.Equal(t, int32(2), *cj.Spec.FailedJobsHistoryLimit)
}

func TestBuildBackupCronJob_TemplateUsesPVC(t *testing.T) {
	inst := cronInstance()
	cj := BuildBackupCronJob(inst)
	vols := cj.Spec.JobTemplate.Spec.Template.Spec.Volumes
	data := volumeByName(vols, "data")
	require.NotNil(t, data)
	require.NotNil(t, data.PersistentVolumeClaim)
	assert.Equal(t, PVCName(inst), data.PersistentVolumeClaim.ClaimName)
}

func TestBuildBackupCronJob_UsesRawS3EnvAndStaticScript(t *testing.T) {
	cj := BuildBackupCronJob(cronInstance())
	c := cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
	cmd := containerCommandText(c)

	assertNoRestic(t, cmd)
	assert.NotContains(t, cmd, "hermes-backups")
	assert.NotContains(t, cmd, "s3.amazonaws.com")
	assert.NotContains(t, cmd, "prod/agents/demo")
	assert.NotContains(t, cmd, "jq")
	assert.Contains(t, cmd, "TIMESTAMP")
	assert.Contains(t, cmd, "SNAPSHOT_PREFIX")
	assert.Contains(t, cmd, `aws --endpoint-url "$S3_ENDPOINT_URL" s3 cp - "s3://$S3_BUCKET/$SNAPSHOT_KEY"`)

	assertEnvValue(t, c.Env, "S3_BUCKET", "hermes-backups")
	assertEnvValue(t, c.Env, "SNAPSHOT_PREFIX", "prod/agents/demo/")
	assertEnvValue(t, c.Env, "S3_ENDPOINT_URL", "https://s3.amazonaws.com")
	assertEnvValue(t, c.Env, "AWS_DEFAULT_REGION", "us-east-1")
}

func TestBuildBackupPruneCronJob_RunsDaily(t *testing.T) {
	cj := BuildBackupPruneCronJob(cronInstance())
	require.NotNil(t, cj)
	assert.Equal(t, "demo-backup-prune", cj.Name)
	assert.Equal(t, "17 4 * * *", cj.Spec.Schedule)
}

func TestBuildBackupCronJob_BackupAndPruneDefaultToAgentImage(t *testing.T) {
	inst := cronInstance()
	backup := BuildBackupCronJob(inst)
	prune := BuildBackupPruneCronJob(inst)
	assert.Equal(t, "ghcr.io/paperclipinc/hermes-agent:v1.2.3", backup.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, "ghcr.io/paperclipinc/hermes-agent:v1.2.3", prune.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image)
}

func TestBuildBackupCronJob_BackupAndPruneHonorCustomImage(t *testing.T) {
	inst := cronInstance()
	inst.Spec.Backup.Image = "internal.registry/hermes-backup:custom"
	backup := BuildBackupCronJob(inst)
	prune := BuildBackupPruneCronJob(inst)
	assert.Equal(t, "internal.registry/hermes-backup:custom", backup.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, "internal.registry/hermes-backup:custom", prune.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image)
}

func TestBuildBackupCronJob_S3CredsViaExplicitSecretKeyRefs(t *testing.T) {
	backup := BuildBackupCronJob(cronInstance())
	prune := BuildBackupPruneCronJob(cronInstance())

	for _, c := range []corev1.Container{
		backup.Spec.JobTemplate.Spec.Template.Spec.Containers[0],
		prune.Spec.JobTemplate.Spec.Template.Spec.Containers[0],
	} {
		assert.Empty(t, c.EnvFrom)
		assertSecretKeyEnv(t, c.Env, "AWS_ACCESS_KEY_ID", "hermes-s3-creds", "S3_ACCESS_KEY_ID")
		assertSecretKeyEnv(t, c.Env, "AWS_SECRET_ACCESS_KEY", "hermes-s3-creds", "S3_SECRET_ACCESS_KEY")
	}
}

func TestBuildBackupCronJob_TmpVolumeAndNoServiceAccountToken(t *testing.T) {
	backup := BuildBackupCronJob(cronInstance())
	prune := BuildBackupPruneCronJob(cronInstance())

	backupPod := backup.Spec.JobTemplate.Spec.Template.Spec
	require.NotNil(t, backupPod.AutomountServiceAccountToken)
	assert.False(t, *backupPod.AutomountServiceAccountToken)
	assertTmpVolumeAndMount(t, backupPod.Volumes, backupPod.Containers[0].VolumeMounts)

	prunePod := prune.Spec.JobTemplate.Spec.Template.Spec
	require.NotNil(t, prunePod.AutomountServiceAccountToken)
	assert.False(t, *prunePod.AutomountServiceAccountToken)
	assertTmpVolumeAndMount(t, prunePod.Volumes, prunePod.Containers[0].VolumeMounts)
}

func TestBuildBackupPruneCronJob_RawObjectPruneCommand(t *testing.T) {
	inst := cronInstance()
	historyLimit := int32(7)
	failedHistoryLimit := int32(2)
	inst.Spec.Backup.HistoryLimit = &historyLimit
	inst.Spec.Backup.FailedHistoryLimit = &failedHistoryLimit

	cj := BuildBackupPruneCronJob(inst)
	c := cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
	cmd := containerCommandText(c)

	assertNoRestic(t, cmd)
	assert.NotContains(t, cmd, "hermes-backups")
	assert.NotContains(t, cmd, "s3.amazonaws.com")
	assert.NotContains(t, cmd, "prod/agents/demo")
	assert.NotContains(t, cmd, "jq")
	assert.Contains(t, cmd, "list-objects-v2")
	assert.Contains(t, cmd, "*.tar.zst")
	assert.Contains(t, cmd, "sort -r")
	assert.Contains(t, cmd, `aws --endpoint-url "$S3_ENDPOINT_URL" s3 rm "s3://$S3_BUCKET/$key"`)

	assertEnvValue(t, c.Env, "S3_BUCKET", "hermes-backups")
	assertEnvValue(t, c.Env, "SNAPSHOT_PREFIX", "prod/agents/demo/")
	assertEnvValue(t, c.Env, "FAILED_SNAPSHOT_PREFIX", "prod/agents/demo/failed/")
	assertEnvValue(t, c.Env, "S3_ENDPOINT_URL", "https://s3.amazonaws.com")
	assertEnvValue(t, c.Env, "AWS_DEFAULT_REGION", "us-east-1")
	assertEnvValue(t, c.Env, "HISTORY_LIMIT", "7")
	assertEnvValue(t, c.Env, "FAILED_HISTORY_LIMIT", "2")
}
