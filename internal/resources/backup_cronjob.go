package resources

import (
	"strconv"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

// BackupCronJobName returns the deterministic name for the periodic backup CronJob.
func BackupCronJobName(inst *hermesv1.HermesInstance) string {
	return inst.Name + "-backup-cron"
}

// BackupPruneCronJobName returns the deterministic name for the history-pruning CronJob.
func BackupPruneCronJobName(inst *hermesv1.HermesInstance) string {
	return inst.Name + "-backup-prune"
}

const pruneBackupScript = `set -eu
: "${S3_BUCKET:?}"
: "${S3_ENDPOINT_URL:?}"
: "${SNAPSHOT_PREFIX:?}"
: "${FAILED_SNAPSHOT_PREFIX:?}"
: "${HISTORY_LIMIT:?}"
: "${FAILED_HISTORY_LIMIT:?}"
: "${AWS_ACCESS_KEY_ID:?}"
: "${AWS_SECRET_ACCESS_KEY:?}"
: "${AWS_DEFAULT_REGION:-}"

WORKDIR="$(mktemp -d /tmp/hermes-prune.XXXXXX)"
cleanup() {
	rm -rf "$WORKDIR"
}
trap cleanup EXIT

prune_prefix() {
	prefix="$1"
	keep="$2"
	exclude="$3"
	keys_file="$WORKDIR/keys"

	aws --endpoint-url "$S3_ENDPOINT_URL" s3api list-objects-v2 \
		--bucket "$S3_BUCKET" \
		--prefix "$prefix" \
		--query 'Contents[].Key' \
		--output text \
		| tr '\t' '\n' \
		| sort -r > "$keys_file"

	seen=0
	while IFS= read -r key; do
		[ -n "$key" ] || continue
		case "$key" in
			*.tar.zst) ;;
			*) continue ;;
		esac
		if [ -n "$exclude" ]; then
			case "$key" in
				"$exclude"*) continue ;;
			esac
		fi
		seen=$((seen + 1))
		if [ "$seen" -le "$keep" ]; then
			continue
		fi
		aws --endpoint-url "$S3_ENDPOINT_URL" s3 rm "s3://$S3_BUCKET/$key" --no-progress
	done < "$keys_file"
}

prune_prefix "$SNAPSHOT_PREFIX" "$HISTORY_LIMIT" "$FAILED_SNAPSHOT_PREFIX"
prune_prefix "$FAILED_SNAPSHOT_PREFIX" "$FAILED_HISTORY_LIMIT" ""
`

// BuildBackupCronJob returns the desired periodic backup CronJob. Caller is
// responsible for setting OwnerReferences and applying via CreateOrUpdate.
func BuildBackupCronJob(inst *hermesv1.HermesInstance) *batchv1.CronJob {
	s3 := inst.Spec.Backup.S3
	image := backupImageRef(inst)

	labels := LabelsForInstance(inst)
	labels["hermes.agent/job-kind"] = "scheduled"

	historyLimit := int32(30)
	if inst.Spec.Backup.HistoryLimit != nil {
		historyLimit = *inst.Spec.Backup.HistoryLimit
	}
	failedHistoryLimit := int32(3)
	if inst.Spec.Backup.FailedHistoryLimit != nil {
		failedHistoryLimit = *inst.Spec.Backup.FailedHistoryLimit
	}

	backoff := int32(3)
	ttl := int32(86400)
	gracePeriod := int64(30)

	args := []string{
		"-c",
		scheduledBackupScript,
	}

	podSpec := corev1.PodSpec{
		RestartPolicy:                 corev1.RestartPolicyOnFailure,
		DNSPolicy:                     corev1.DNSClusterFirst,
		SchedulerName:                 "default-scheduler",
		TerminationGracePeriodSeconds: &gracePeriod,
		AutomountServiceAccountToken:  Ptr(false),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: Ptr(true),
			RunAsUser:    Ptr(int64(1000)),
			RunAsGroup:   Ptr(int64(1000)),
			FSGroup:      Ptr(int64(1000)),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Affinity: backupPodAffinity(inst),
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
				corev1.EnvVar{Name: "SNAPSHOT_PREFIX", Value: backupObjectPrefix(inst)},
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
	}

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackupCronJobName(inst),
			Namespace: inst.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   inst.Spec.Backup.Schedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: &historyLimit,
			FailedJobsHistoryLimit:     &failedHistoryLimit,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: batchv1.JobSpec{
					BackoffLimit:            &backoff,
					TTLSecondsAfterFinished: &ttl,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       podSpec,
					},
				},
			},
		},
	}
}

// backupPodAffinity co-locates backup pods with the agent StatefulSet pod so
// read/write-once PVCs can be mounted for both scheduled and one-shot backups.
func backupPodAffinity(inst *hermesv1.HermesInstance) *corev1.Affinity {
	return &corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app.kubernetes.io/name":     "hermes-agent",
						"app.kubernetes.io/instance": inst.Name,
					},
				},
				TopologyKey: "kubernetes.io/hostname",
			}},
		},
	}
}

// BuildBackupPruneCronJob returns a daily CronJob that purges old snapshots.
//
// The prune logic:
//   - Lists `<prefix><ns>/<name>/*.tar.zst` sorted desc by lex timestamp.
//   - Keeps the newest `historyLimit`; deletes the rest.
//   - Lists `<prefix><ns>/<name>/failed/*.tar.zst` similarly with `failedHistoryLimit`.
func BuildBackupPruneCronJob(inst *hermesv1.HermesInstance) *batchv1.CronJob {
	s3 := inst.Spec.Backup.S3
	image := backupImageRef(inst)
	labels := LabelsForInstance(inst)
	labels["hermes.agent/job-kind"] = "prune"

	historyLimit := int32(30)
	if inst.Spec.Backup.HistoryLimit != nil {
		historyLimit = *inst.Spec.Backup.HistoryLimit
	}
	failedHistoryLimit := int32(3)
	if inst.Spec.Backup.FailedHistoryLimit != nil {
		failedHistoryLimit = *inst.Spec.Backup.FailedHistoryLimit
	}

	successLim := int32(1)
	failLim := int32(3)

	backoff := int32(2)
	ttl := int32(86400)

	args := []string{
		"-c",
		pruneBackupScript,
	}

	podSpec := corev1.PodSpec{
		RestartPolicy:                 corev1.RestartPolicyOnFailure,
		DNSPolicy:                     corev1.DNSClusterFirst,
		SchedulerName:                 "default-scheduler",
		TerminationGracePeriodSeconds: Ptr(int64(30)),
		AutomountServiceAccountToken:  Ptr(false),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   Ptr(true),
			RunAsUser:      Ptr(int64(1000)),
			RunAsGroup:     Ptr(int64(1000)),
			FSGroup:        Ptr(int64(1000)),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		Containers: []corev1.Container{{
			Name:                     "prune",
			Image:                    image,
			ImagePullPolicy:          corev1.PullIfNotPresent,
			Command:                  []string{"/bin/sh"},
			Args:                     args,
			TerminationMessagePath:   "/dev/termination-log",
			TerminationMessagePolicy: corev1.TerminationMessageReadFile,
			Env: backupS3Env(s3,
				corev1.EnvVar{Name: "SNAPSHOT_PREFIX", Value: backupObjectPrefix(inst)},
				corev1.EnvVar{Name: "FAILED_SNAPSHOT_PREFIX", Value: failedBackupObjectPrefix(inst)},
				corev1.EnvVar{Name: "HISTORY_LIMIT", Value: strconv.FormatInt(int64(historyLimit), 10)},
				corev1.EnvVar{Name: "FAILED_HISTORY_LIMIT", Value: strconv.FormatInt(int64(failedHistoryLimit), 10)},
			),
			VolumeMounts: []corev1.VolumeMount{
				{Name: "tmp", MountPath: "/tmp"},
			},
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: Ptr(false),
				ReadOnlyRootFilesystem:   Ptr(true),
				Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			},
		}},
		Volumes: []corev1.Volume{{
			Name: "tmp",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}},
	}

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackupPruneCronJobName(inst),
			Namespace: inst.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   "17 4 * * *",
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: &successLim,
			FailedJobsHistoryLimit:     &failLim,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: batchv1.JobSpec{
					BackoffLimit:            &backoff,
					TTLSecondsAfterFinished: &ttl,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       podSpec,
					},
				},
			},
		},
	}
}
