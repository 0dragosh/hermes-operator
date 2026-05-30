package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func instWithRuntime(r hermesv1.RuntimeSpec) *hermesv1.HermesInstance {
	return &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec:       hermesv1.HermesInstanceSpec{Runtime: r},
	}
}

func TestBuildRuntimeInitContainers_UVDefault(t *testing.T) {
	inst := instWithRuntime(hermesv1.RuntimeSpec{
		UV: hermesv1.UVSpec{Enabled: Ptr(true)},
	})
	got := BuildRuntimeInitContainers(inst)
	assert.Len(t, got, 1, "uv-sync only")
	assert.Equal(t, "init-uv", got[0].Name)
	assert.Contains(t, got[0].Command[2], "uv sync --frozen", "frozen lockfile sync")
	hasFullData := false
	for _, m := range got[0].VolumeMounts {
		if m.Name == "data" && m.MountPath == "/home/hermes/.hermes" && m.SubPath == "" {
			hasFullData = true
		}
	}
	assert.True(t, hasFullData, "init container must mount the full data volume without subPath")
	assert.True(t, hasVolumeMount(got[0], "tmp", "/tmp"), "init-uv needs writable /tmp")
}

func TestBuildRuntimeInitContainers_UVDisabled(t *testing.T) {
	inst := instWithRuntime(hermesv1.RuntimeSpec{
		UV: hermesv1.UVSpec{Enabled: Ptr(false)},
	})
	got := BuildRuntimeInitContainers(inst)
	for _, c := range got {
		assert.NotEqual(t, "init-uv", c.Name, "uv sync should be skipped when disabled")
	}
}

func TestBuildRuntimeInitContainers_UnsupportedExtraAptDoesNotEmitInitApt(t *testing.T) {
	inst := instWithRuntime(hermesv1.RuntimeSpec{
		UV:               hermesv1.UVSpec{Enabled: Ptr(true)},
		ExtraAptPackages: []string{"poppler-utils", "tesseract-ocr"},
	})
	got := BuildRuntimeInitContainers(inst)
	for _, c := range got {
		assert.NotEqual(t, "init-apt", c.Name)
	}
	assert.NotNil(t, containerByName(got, "init-uv"), "uv init still emits when enabled")
}

func TestBuildRuntimeInitContainers_ExtraPip(t *testing.T) {
	inst := instWithRuntime(hermesv1.RuntimeSpec{
		UV:               hermesv1.UVSpec{Enabled: Ptr(true)},
		ExtraPipPackages: []string{"pandas==2.2.0", "polars"},
	})
	got := BuildRuntimeInitContainers(inst)
	var pipC *corev1.Container
	for i, c := range got {
		if c.Name == "init-pip" {
			pipC = &got[i]
		}
	}
	if !assert.NotNil(t, pipC, "init-pip container missing") {
		return
	}
	assert.Contains(t, pipC.Command[2], "uv pip install")
	assert.Contains(t, pipC.Command[2], "pandas==2.2.0")
	assert.Contains(t, pipC.Command[2], "polars")
	assert.Contains(t, pipC.Command[2], "/home/hermes/.hermes/.venv-extras")
	assert.True(t, hasVolumeMount(*pipC, "tmp", "/tmp"), "init-pip needs writable /tmp")
}

func TestBuildRuntimeInitContainers_Order(t *testing.T) {
	inst := instWithRuntime(hermesv1.RuntimeSpec{
		UV:               hermesv1.UVSpec{Enabled: Ptr(true)},
		ExtraAptPackages: []string{"libxml2-dev"},
		ExtraPipPackages: []string{"lxml"},
	})
	got := BuildRuntimeInitContainers(inst)
	names := []string{}
	for _, c := range got {
		names = append(names, c.Name)
	}
	assert.Equal(t, []string{"init-uv", "init-pip"}, names)
}

func TestBuildRuntimeInitContainers_BootstrapAbsentByDefault(t *testing.T) {
	inst := instWithRuntime(hermesv1.RuntimeSpec{
		UV: hermesv1.UVSpec{Enabled: Ptr(false)},
	})

	got := BuildRuntimeInitContainers(inst)

	assert.Nil(t, containerByName(got, "init-workspace-bootstrap"))
}

func TestBuildRuntimeInitContainers_BootstrapPresentWhenEnabled(t *testing.T) {
	inst := instWithRuntime(hermesv1.RuntimeSpec{
		UV: hermesv1.UVSpec{Enabled: Ptr(false)},
	})
	inst.Spec.Workspace.Bootstrap.Enabled = Ptr(true)

	got := BuildRuntimeInitContainers(inst)
	bootstrap := containerByName(got, "init-workspace-bootstrap")
	if !assert.NotNil(t, bootstrap) {
		return
	}

	assert.Contains(t, bootstrap.Command[2], "hermes onboard")
	assert.Contains(t, bootstrap.Command[2], "touch \"$SENTINEL\"")
	assert.Contains(t, bootstrap.Command[2], "\"$SEED_DIR\"/.[!.]*")
	assert.NotContains(t, bootstrap.Command[2], "/home/hermes/.hermes-workspace-seed")
	assert.NotContains(t, bootstrap.Command[2], InitialDirsKey)
	assert.Equal(t, "/home/hermes/.hermes", envValue(*bootstrap, "HERMES_DATA_DIR"))
	assert.Equal(t, "/home/hermes/.hermes-workspace-seed", envValue(*bootstrap, "HERMES_WORKSPACE_SEED_DIR"))
	assert.Equal(t, InitialDirsKey, envValue(*bootstrap, "HERMES_INITIAL_DIRS_KEY"))
	assert.Equal(t, "/home/hermes/.hermes/.bootstrap-complete", envValue(*bootstrap, "HERMES_BOOTSTRAP_SENTINEL"))
	assert.True(t, hasVolumeMount(*bootstrap, "data", "/home/hermes/.hermes"))
	assert.True(t, hasVolumeMount(*bootstrap, "workspace", "/home/hermes/.hermes-workspace-seed"))
	assert.True(t, hasVolumeMount(*bootstrap, "tmp", "/tmp"))
}

func TestBuildRuntimeVolumes_UVCacheEmptyDirDefault(t *testing.T) {
	inst := instWithRuntime(hermesv1.RuntimeSpec{
		UV: hermesv1.UVSpec{Enabled: Ptr(true)},
	})
	vols := BuildRuntimeVolumes(inst)
	found := false
	for _, v := range vols {
		if v.Name == "uv-cache" {
			found = true
			assert.NotNil(t, v.EmptyDir, "default to emptyDir")
		}
	}
	assert.True(t, found, "uv-cache volume present when uv enabled")
}

func TestBuildRuntimeVolumes_ExtraPipNeedsUVCacheWhenUVDisabled(t *testing.T) {
	inst := instWithRuntime(hermesv1.RuntimeSpec{
		UV:               hermesv1.UVSpec{Enabled: Ptr(false)},
		ExtraPipPackages: []string{"pandas"},
	})

	vols := BuildRuntimeVolumes(inst)

	assert.NotNil(t, volumeByName(vols, "uv-cache"), "init-pip mounts uv-cache even when uv sync is disabled")
}

func containerByName(containers []corev1.Container, name string) *corev1.Container {
	for i, c := range containers {
		if c.Name == name {
			return &containers[i]
		}
	}
	return nil
}

func hasVolumeMount(container corev1.Container, name, mountPath string) bool {
	for _, m := range container.VolumeMounts {
		if m.Name == name && m.MountPath == mountPath {
			return true
		}
	}
	return false
}

func envValue(container corev1.Container, name string) string {
	for _, env := range container.Env {
		if env.Name == name {
			return env.Value
		}
	}
	return ""
}
