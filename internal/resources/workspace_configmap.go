package resources

import (
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

// InitialDirsKey is the well-known data key holding the newline-separated list
// of directories to mkdir -p.
const InitialDirsKey = "__hermes_initial_dirs__"

// WorkspaceConfigMapName returns the deterministic name.
func WorkspaceConfigMapName(inst *hermesv1.HermesInstance) string {
	return inst.Name + "-workspace"
}

// EncodeWorkspacePath turns a relative path into a ConfigMap-safe key. It
// escapes "_" before "/" so DecodeWorkspacePath can invert paths containing
// underscores, including underscores adjacent to path separators.
func EncodeWorkspacePath(path string) string {
	path = strings.ReplaceAll(path, "_", "_u")
	return strings.ReplaceAll(path, "/", "_s")
}

// DecodeWorkspacePath is the inverse of EncodeWorkspacePath.
func DecodeWorkspacePath(key string) string {
	key = strings.ReplaceAll(key, "_s", "/")
	return strings.ReplaceAll(key, "_u", "_")
}

// BuildWorkspaceConfigMap creates the ConfigMap holding base data,
// spec.workspace.initialFiles (path-encoded into ConfigMap data keys), and
// spec.workspace.initialDirs (under a single newline-separated key). Base data
// is copied first, and spec.workspace.initialFiles wins on key conflicts.
func BuildWorkspaceConfigMap(inst *hermesv1.HermesInstance, base map[string]string) *corev1.ConfigMap {
	data := make(map[string]string, len(base)+len(inst.Spec.Workspace.InitialFiles)+1)
	for k, v := range base {
		data[k] = v
	}
	for _, f := range inst.Spec.Workspace.InitialFiles {
		data[EncodeWorkspacePath(f.Path)] = f.Content
	}
	dirs := make([]string, len(inst.Spec.Workspace.InitialDirs))
	copy(dirs, inst.Spec.Workspace.InitialDirs)
	sort.Strings(dirs)
	if len(dirs) == 0 {
		data[InitialDirsKey] = ""
	} else {
		data[InitialDirsKey] = strings.Join(dirs, "\n") + "\n"
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      WorkspaceConfigMapName(inst),
			Namespace: inst.Namespace,
			Labels:    LabelsForInstance(inst),
		},
		Data: data,
	}
}
