package resources

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

const workspaceBootstrapScript = `set -eu
DATA_DIR="${HERMES_DATA_DIR:?}"
SEED_DIR="${HERMES_WORKSPACE_SEED_DIR:?}"
DIRS_KEY="${HERMES_INITIAL_DIRS_KEY:?}"
SENTINEL="${HERMES_BOOTSTRAP_SENTINEL:?}"

mkdir -p "$DATA_DIR"
if [ -e "$SENTINEL" ]; then
  exit 0
fi

if [ -f "$SEED_DIR/$DIRS_KEY" ]; then
  while IFS= read -r rel_dir || [ -n "$rel_dir" ]; do
    [ -n "$rel_dir" ] || continue
    case "$rel_dir" in
      /*|.|..|../*|*/../*|*/..) echo "refusing unsafe initial dir: $rel_dir" >&2; exit 1 ;;
    esac
    mkdir -p "$DATA_DIR/$rel_dir"
  done < "$SEED_DIR/$DIRS_KEY"
fi

for src in "$SEED_DIR"/* "$SEED_DIR"/.[!.]* "$SEED_DIR"/..?*; do
  [ -e "$src" ] || continue
  [ -f "$src" ] || continue
  name="$(basename "$src")"
  [ "$name" != "$DIRS_KEY" ] || continue
  dest_rel="$(printf '%s' "$name" | sed 's#_s#/#g; s#_u#_#g')"
  case "$dest_rel" in
    /*|.|..|../*|*/../*|*/..) echo "refusing unsafe seed path: $dest_rel" >&2; exit 1 ;;
  esac
  mkdir -p "$(dirname "$DATA_DIR/$dest_rel")"
  cp "$src" "$DATA_DIR/$dest_rel"
done

hermes onboard
touch "$SENTINEL"
`

// BuildRuntimeInitContainers returns the ordered init containers required by
// workspace bootstrap and spec.runtime. Order: init-workspace-bootstrap,
// init-uv, init-pip. Each container mounts the full data volume (no subPath,
// lesson openclaw #450).
func BuildRuntimeInitContainers(inst *hermesv1.HermesInstance) []corev1.Container {
	var out []corev1.Container
	if bootstrapEnabled(inst) {
		out = append(out, buildWorkspaceBootstrapInit(inst))
	}
	if uvEnabled(inst) {
		out = append(out, buildUVSyncInit(inst))
	}
	if len(inst.Spec.Runtime.ExtraPipPackages) > 0 {
		out = append(out, buildPipInit(inst))
	}
	return out
}

// BuildRuntimeVolumes returns additional Volumes beyond data PVC + config CM.
func BuildRuntimeVolumes(inst *hermesv1.HermesInstance) []corev1.Volume {
	var out []corev1.Volume
	if !uvCacheNeeded(inst) {
		return out
	}
	cache := inst.Spec.Runtime.UV.CacheVolume
	vol := corev1.Volume{Name: "uv-cache"}
	switch {
	case cache.PersistentVolumeClaim != nil:
		vol.VolumeSource = corev1.VolumeSource{PersistentVolumeClaim: cache.PersistentVolumeClaim}
	case cache.EmptyDir != nil:
		vol.VolumeSource = corev1.VolumeSource{EmptyDir: cache.EmptyDir}
	default:
		size := resource.MustParse("1Gi")
		vol.VolumeSource = corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &size}}
	}
	out = append(out, vol)
	return out
}

// BuildRuntimeVolumeMounts returns the additional mounts for the main hermes
// container.
func BuildRuntimeVolumeMounts(inst *hermesv1.HermesInstance) []corev1.VolumeMount {
	if !uvEnabled(inst) {
		return nil
	}
	return []corev1.VolumeMount{
		{Name: "uv-cache", MountPath: "/home/hermes/.cache/uv"},
	}
}

func uvEnabled(inst *hermesv1.HermesInstance) bool {
	if inst.Spec.Runtime.UV.Enabled == nil {
		return true
	}
	return *inst.Spec.Runtime.UV.Enabled
}

func uvCacheNeeded(inst *hermesv1.HermesInstance) bool {
	return uvEnabled(inst) || len(inst.Spec.Runtime.ExtraPipPackages) > 0
}

func bootstrapEnabled(inst *hermesv1.HermesInstance) bool {
	return BoolValue(inst.Spec.Workspace.Bootstrap.Enabled)
}

func dataVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{Name: "data", MountPath: "/home/hermes/.hermes"}
}

func buildUVSyncInit(inst *hermesv1.HermesInstance) corev1.Container {
	extra := inst.Spec.Runtime.UV.ExtraIndexURL
	indexArg := ""
	if extra != "" {
		indexArg = fmt.Sprintf("--extra-index-url=%s ", shellQuote(extra))
	}
	cmd := fmt.Sprintf(
		"set -eu; cd /home/hermes/.hermes; cp /opt/venv-template/pyproject.toml /opt/venv-template/uv.lock .; uv sync --frozen %s",
		indexArg,
	)
	return corev1.Container{
		Name:                     "init-uv",
		Image:                    imageRef(inst),
		ImagePullPolicy:          pullPolicy(inst),
		Command:                  []string{"/bin/sh", "-c", cmd},
		TerminationMessagePath:   "/dev/termination-log",
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             Ptr(true),
			RunAsUser:                Ptr(int64(1000)),
			AllowPrivilegeEscalation: Ptr(false),
			ReadOnlyRootFilesystem:   Ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		VolumeMounts: []corev1.VolumeMount{
			dataVolumeMount(),
			{Name: "uv-cache", MountPath: "/home/hermes/.cache/uv"},
			{Name: "tmp", MountPath: "/tmp"},
		},
	}
}

func buildWorkspaceBootstrapInit(inst *hermesv1.HermesInstance) corev1.Container {
	return corev1.Container{
		Name:                     "init-workspace-bootstrap",
		Image:                    imageRef(inst),
		ImagePullPolicy:          pullPolicy(inst),
		Command:                  []string{"/bin/sh", "-c", workspaceBootstrapScript},
		TerminationMessagePath:   "/dev/termination-log",
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		Env: []corev1.EnvVar{
			{Name: "HERMES_DATA_DIR", Value: "/home/hermes/.hermes"},
			{Name: "HERMES_WORKSPACE_SEED_DIR", Value: "/home/hermes/.hermes-workspace-seed"},
			{Name: "HERMES_INITIAL_DIRS_KEY", Value: InitialDirsKey},
			{Name: "HERMES_BOOTSTRAP_SENTINEL", Value: "/home/hermes/.hermes/.bootstrap-complete"},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             Ptr(true),
			RunAsUser:                Ptr(int64(1000)),
			AllowPrivilegeEscalation: Ptr(false),
			ReadOnlyRootFilesystem:   Ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		VolumeMounts: []corev1.VolumeMount{
			dataVolumeMount(),
			{Name: "workspace", MountPath: "/home/hermes/.hermes-workspace-seed", ReadOnly: true},
			{Name: "tmp", MountPath: "/tmp"},
		},
	}
}

func buildPipInit(inst *hermesv1.HermesInstance) corev1.Container {
	venvPath := "/home/hermes/.hermes/.venv-extras"
	pkgs := strings.Join(quoteEach(inst.Spec.Runtime.ExtraPipPackages), " ")
	cmd := fmt.Sprintf(
		"set -eu; test -d %[1]s || uv venv %[1]s; VIRTUAL_ENV=%[1]s uv pip install %[2]s",
		venvPath, pkgs,
	)
	return corev1.Container{
		Name:                     "init-pip",
		Image:                    imageRef(inst),
		ImagePullPolicy:          pullPolicy(inst),
		Command:                  []string{"/bin/sh", "-c", cmd},
		TerminationMessagePath:   "/dev/termination-log",
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             Ptr(true),
			RunAsUser:                Ptr(int64(1000)),
			AllowPrivilegeEscalation: Ptr(false),
			ReadOnlyRootFilesystem:   Ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		VolumeMounts: []corev1.VolumeMount{
			dataVolumeMount(),
			{Name: "uv-cache", MountPath: "/home/hermes/.cache/uv"},
			{Name: "tmp", MountPath: "/tmp"},
		},
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func quoteEach(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = shellQuote(s)
	}
	return out
}
