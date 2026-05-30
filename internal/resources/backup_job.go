package resources

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

// BackupJobOpts captures the inputs the controller passes to the builder.
type BackupJobOpts struct {
	Name        string // Deterministic Job name (e.g. "<inst>-backup-final")
	SnapshotKey string // Full S3 key the snapshot will be written to
	Kind        string // "onDelete" | "preUpdate" | "scheduled": recorded as a label
}

// BuildBackupOneShotJob returns a Job that snapshots the instance PVC to S3.
func BuildBackupOneShotJob(inst *hermesv1.HermesInstance, opts BackupJobOpts) *batchv1.Job {
	image := backupImageRef(inst)
	s3 := inst.Spec.Backup.S3

	labels := LabelsForInstance(inst)
	labels["hermes.agent/job-kind"] = opts.Kind

	backoff := int32(3)
	ttl := int32(86400)

	args := []string{
		"-c",
		oneShotBackupScript,
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Name,
			Namespace: inst.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:                 corev1.RestartPolicyOnFailure,
					DNSPolicy:                     corev1.DNSClusterFirst,
					SchedulerName:                 "default-scheduler",
					TerminationGracePeriodSeconds: Ptr(int64(30)),
					AutomountServiceAccountToken:  Ptr(false),
					Affinity:                      backupPodAffinity(inst),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: Ptr(true),
						RunAsUser:    Ptr(int64(1000)),
						RunAsGroup:   Ptr(int64(1000)),
						FSGroup:      Ptr(int64(1000)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Name:                     "backup",
						Image:                    image,
						ImagePullPolicy:          corev1.PullIfNotPresent,
						Command:                  []string{"/bin/sh"},
						Args:                     args,
						TerminationMessagePath:   "/dev/termination-log",
						TerminationMessagePolicy: corev1.TerminationMessageReadFile,
						Env: backupS3Env(s3,
							corev1.EnvVar{Name: "INSTANCE_UID", Value: string(inst.UID)},
							corev1.EnvVar{Name: "SNAPSHOT_KEY", Value: opts.SnapshotKey},
						),
						VolumeMounts: []corev1.VolumeMount{
							{Name: "data", MountPath: "/home/hermes/.hermes", ReadOnly: true},
							{Name: "tmp", MountPath: "/tmp"},
						},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: Ptr(false),
							ReadOnlyRootFilesystem:   Ptr(true),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: PVCName(inst),
									ReadOnly:  true,
								},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}

const backupScriptBody = `: "${S3_BUCKET:?}"
: "${S3_ENDPOINT_URL:?}"
: "${SNAPSHOT_KEY:?}"
: "${AWS_ACCESS_KEY_ID:?}"
: "${AWS_SECRET_ACCESS_KEY:?}"
: "${AWS_DEFAULT_REGION:-}"

TIMESTAMP="${TIMESTAMP:-$(date -u +%Y-%m-%dT%H-%M-%SZ)}"
WORKDIR="$(mktemp -d /tmp/hermes-backup.XXXXXX)"
cleanup() {
	rm -rf "$WORKDIR"
}
trap cleanup EXIT

cat > "$WORKDIR/meta.json" <<EOF
{"instance_uid":"$INSTANCE_UID","timestamp":"$TIMESTAMP","format_version":1}
EOF
tar -C /home/hermes/.hermes -cf "$WORKDIR/hermes.tar" .
tar -C "$WORKDIR" -rf "$WORKDIR/hermes.tar" meta.json
zstd -T0 -19 -c "$WORKDIR/hermes.tar" > "$WORKDIR/hermes.tar.zst"
aws --endpoint-url "$S3_ENDPOINT_URL" s3 cp - "s3://$S3_BUCKET/$SNAPSHOT_KEY" --no-progress < "$WORKDIR/hermes.tar.zst"
`

const oneShotBackupScript = `set -eu
` + backupScriptBody

const scheduledBackupScript = `set -eu
: "${SNAPSHOT_PREFIX:?}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
SNAPSHOT_KEY="${SNAPSHOT_PREFIX}${TIMESTAMP}.tar.zst"
export SNAPSHOT_KEY TIMESTAMP

` + backupScriptBody

func backupImageRef(inst *hermesv1.HermesInstance) string {
	if inst.Spec.Backup.Image != "" {
		return inst.Spec.Backup.Image
	}
	return imageRef(inst)
}

func backupS3Env(s3 *hermesv1.BackupS3Spec, extra ...corev1.EnvVar) []corev1.EnvVar {
	bucket := ""
	endpoint := ""
	region := ""
	secretName := ""
	if s3 != nil {
		bucket = s3.Bucket
		endpoint = normalizedEndpointURL(s3.Endpoint)
		region = s3.Region
		secretName = s3.CredentialsSecretRef.Name
	}
	env := []corev1.EnvVar{
		{Name: "S3_BUCKET", Value: bucket},
		{Name: "S3_ENDPOINT_URL", Value: endpoint},
		{Name: "AWS_DEFAULT_REGION", Value: region},
		{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "S3_ACCESS_KEY_ID",
				},
			},
		},
		{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "S3_SECRET_ACCESS_KEY",
				},
			},
		},
	}
	return append(env, extra...)
}

func backupObjectPrefix(inst *hermesv1.HermesInstance) string {
	prefix := ""
	if inst.Spec.Backup.S3 != nil {
		prefix = inst.Spec.Backup.S3.PathPrefix
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}
	return fmt.Sprintf("%s%s/%s/", prefix, inst.Namespace, inst.Name)
}

func failedBackupObjectPrefix(inst *hermesv1.HermesInstance) string {
	return backupObjectPrefix(inst) + "failed/"
}
