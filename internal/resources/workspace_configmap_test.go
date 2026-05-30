package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func TestEncodeWorkspacePath_FlatAndNested(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "shallow.txt", EncodeWorkspacePath("shallow.txt"))
	assert.Equal(t, "notes_sfinance_s2026.md", EncodeWorkspacePath("notes/finance/2026.md"))
	assert.Equal(t, "deep_sa_sb_sc_sd.txt", EncodeWorkspacePath("deep/a/b/c/d.txt"))
	assert.Equal(t, "notes_sfinance_u2026.md", EncodeWorkspacePath("notes/finance_2026.md"))
}

func TestDecodeWorkspacePath_Roundtrip(t *testing.T) {
	t.Parallel()
	cases := []string{"a.md", "a/b.md", "a/b/c/d/e/f.md", "a_b.md", "a_/b", "a/_b", ".profile"}
	for _, p := range cases {
		got := DecodeWorkspacePath(EncodeWorkspacePath(p))
		assert.Equal(t, p, got, "round-trip failed for %q", p)
	}
}

func TestBuildWorkspaceConfigMap_Encoded(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Workspace: hermesv1.WorkspaceSpec{
				InitialFiles: []hermesv1.WorkspaceFile{
					{Path: "notes/finance.md", Content: "Q1"},
					{Path: "shallow.txt", Content: "ok"},
				},
				InitialDirs: []string{"data", "data/raw"},
			},
		},
	}
	cm := BuildWorkspaceConfigMap(inst, nil)
	assert.Equal(t, "demo-workspace", cm.Name)
	assert.Equal(t, "Q1", cm.Data["notes_sfinance.md"])
	assert.Equal(t, "ok", cm.Data["shallow.txt"])
	assert.Equal(t, "data\ndata/raw\n", cm.Data[InitialDirsKey])
}

func TestBuildWorkspaceConfigMap_EmptyIsStillEmitted(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: "demo"}}
	cm := BuildWorkspaceConfigMap(inst, nil)
	assert.Equal(t, "demo-workspace", cm.Name)
	assert.NotNil(t, cm.Data)
	assert.Contains(t, cm.Data, InitialDirsKey)
	assert.Equal(t, "", cm.Data[InitialDirsKey])
}

func TestBuildWorkspaceConfigMap_PreservesBaseData(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: "demo"}}
	base := map[string]string{"external.txt": "from-base"}

	cm := BuildWorkspaceConfigMap(inst, base)

	assert.Equal(t, "from-base", cm.Data["external.txt"])
	base["external.txt"] = "mutated"
	assert.Equal(t, "from-base", cm.Data["external.txt"], "builder must copy base data")
}

func TestBuildWorkspaceConfigMap_InitialFilesWinOnConflict(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Workspace: hermesv1.WorkspaceSpec{
				InitialFiles: []hermesv1.WorkspaceFile{{
					Path:    "notes/finance.md",
					Content: "from-spec",
				}},
			},
		},
	}
	base := map[string]string{"notes_sfinance.md": "from-base"}

	cm := BuildWorkspaceConfigMap(inst, base)

	assert.Equal(t, "from-spec", cm.Data["notes_sfinance.md"])
}

func TestBuildWorkspaceConfigMap_InitialDirsKeyIsDeterministic(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Workspace: hermesv1.WorkspaceSpec{
				InitialDirs: []string{"zeta", "alpha/beta", "alpha"},
			},
		},
	}
	base := map[string]string{InitialDirsKey: "stale\n"}

	cm := BuildWorkspaceConfigMap(inst, base)

	assert.Equal(t, "alpha\nalpha/beta\nzeta\n", cm.Data[InitialDirsKey])
}
