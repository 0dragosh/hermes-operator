package resources

import (
	corev1 "k8s.io/api/core/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

const restoreFromS3Script = `set -euo pipefail
DEST=/home/hermes/.hermes
HAS_RESTORE_CONTENTS=false
for entry in "$DEST"/* "$DEST"/.[!.]* "$DEST"/..?*; do
  [ -e "$entry" ] || continue
  case "$(basename "$entry")" in
    lost+found) continue ;;
  esac
  HAS_RESTORE_CONTENTS=true
  break
done
if [ "$HAS_RESTORE_CONTENTS" = true ] && [ -z "${HERMES_RESTORE_FORCE:-}" ]; then
  echo "ERROR: restore destination $DEST is not empty; refusing to overwrite. Set HERMES_RESTORE_FORCE=1 to override." >&2
  exit 1
fi
aws --endpoint-url "$S3_ENDPOINT_URL" s3 cp "s3://${S3_BUCKET}/${SNAPSHOT_KEY}" - --no-progress \
  | zstd -d \
  | tar -xf - -C "$DEST"
echo "restore complete: $SNAPSHOT_KEY -> $DEST" >&2
`

// BuildRestoreInitContainer returns the init container that restores a snapshot
// into the PVC. Returns nil when no restore is requested or one already finished.
func BuildRestoreInitContainer(inst *hermesv1.HermesInstance) *corev1.Container {
	if inst.Spec.RestoreFrom == "" {
		return nil
	}
	if inst.Status.RestoredFrom == inst.Spec.RestoreFrom {
		return nil
	}
	if inst.Spec.Backup.S3 == nil {
		return nil
	}

	s3 := inst.Spec.Backup.S3
	image := inst.Spec.Backup.Image
	if image == "" {
		image = imageRef(inst)
	}

	args := []string{
		"-c",
		restoreFromS3Script,
	}

	return &corev1.Container{
		Name:                     "init-restore",
		Image:                    image,
		ImagePullPolicy:          corev1.PullIfNotPresent,
		Command:                  []string{"/bin/sh"},
		Args:                     args,
		TerminationMessagePath:   "/dev/termination-log",
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		Env: append([]corev1.EnvVar{
			{Name: "S3_BUCKET", Value: s3.Bucket},
			{Name: "SNAPSHOT_KEY", Value: inst.Spec.RestoreFrom},
			{Name: "S3_ENDPOINT_URL", Value: normalizedEndpointURL(s3.Endpoint)},
			{Name: "AWS_DEFAULT_REGION", Value: s3.Region},
		}, s3CredentialKeyRefEnv(s3.CredentialsSecretRef.Name)...),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: "/home/hermes/.hermes"},
			{Name: "tmp", MountPath: "/tmp"},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: Ptr(false),
			ReadOnlyRootFilesystem:   Ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}
}

func s3CredentialKeyRefEnv(secretName string) []corev1.EnvVar {
	return []corev1.EnvVar{
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
}
