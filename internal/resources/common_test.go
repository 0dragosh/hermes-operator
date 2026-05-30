package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func TestPtr(t *testing.T) {
	s := Ptr("x")
	assert.NotNil(t, s)
	assert.Equal(t, "x", *s)
}

func TestLabelsForInstance(t *testing.T) {
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
	}
	got := LabelsForInstance(inst)
	assert.Equal(t, "hermes-agent", got["app.kubernetes.io/name"])
	assert.Equal(t, "demo", got["app.kubernetes.io/instance"])
	assert.Equal(t, "hermes-operator", got["app.kubernetes.io/managed-by"])
	assert.Equal(t, "hermes.agent", got["app.kubernetes.io/part-of"])
}

func TestMergePreservingForeignAnnotations(t *testing.T) {
	existing := map[string]string{
		"hermes.agent/foo":    "old",
		"third-party/keep-me": "preserve",
	}
	desired := map[string]string{
		"hermes.agent/foo": "new",
		"hermes.agent/bar": "added",
	}
	got := MergePreservingForeign(existing, desired, "hermes.agent/")
	assert.Equal(t, "new", got["hermes.agent/foo"], "operator key overwritten")
	assert.Equal(t, "added", got["hermes.agent/bar"], "new operator key added")
	assert.Equal(t, "preserve", got["third-party/keep-me"], "foreign key preserved")
}

func TestPortConstants(t *testing.T) {
	t.Parallel()
	// Constants must be stable: Plan 3-6 reference these by name.
	assert.Equal(t, int32(8443), GatewayPort)
	assert.Equal(t, int32(9090), DefaultMetricsPort)
	assert.Equal(t, "gateway", GatewayPortName)
	assert.Equal(t, "metrics", MetricsPortName)
}

func TestSelectorLabels(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns"}}
	got := SelectorLabels(inst)
	// Selector labels are the immutable subset of LabelsForInstance.
	assert.Equal(t, "hermes-agent", got["app.kubernetes.io/name"])
	assert.Equal(t, "demo", got["app.kubernetes.io/instance"])
	// Selector labels MUST NOT include "managed-by" because that field is
	// allowed to evolve across operator versions.
	_, exists := got["app.kubernetes.io/managed-by"]
	assert.False(t, exists)
}

func TestServiceAccountName_Override(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Security: hermesv1.SecuritySpec{
				RBAC: hermesv1.RBACSpec{ServiceAccountName: "byo-sa"},
			},
		},
	}
	assert.Equal(t, "byo-sa", ServiceAccountNameFor(inst))

	inst.Spec.Security.RBAC.ServiceAccountName = ""
	assert.Equal(t, "demo", ServiceAccountNameFor(inst))
}

func TestBoolValue(t *testing.T) {
	t.Parallel()
	assert.True(t, BoolValue(Ptr(true)))
	assert.False(t, BoolValue(Ptr(false)))
	assert.False(t, BoolValue(nil))
	assert.True(t, BoolValueOrDefault(nil, true))
	assert.False(t, BoolValueOrDefault(Ptr(false), true))
}

func TestPersistenceEnabled_DefaultAndDisabled(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{}

	assert.True(t, PersistenceEnabled(inst), "persistence defaults on")

	inst.Spec.Storage.Persistence.Enabled = Ptr(false)
	assert.False(t, PersistenceEnabled(inst), "explicit false disables persistence")
}

func TestMetricsEnabled_DefaultAndDisabled(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{}

	assert.True(t, MetricsEnabled(inst), "metrics default on")

	inst.Spec.Observability.Metrics.Enabled = Ptr(false)
	assert.False(t, MetricsEnabled(inst), "explicit false disables metrics")
}

func TestEffectiveMetricsPort_DefaultAndCustom(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{}

	assert.Equal(t, DefaultMetricsPort, EffectiveMetricsPort(inst))

	inst.Spec.Observability.Metrics.Port = 9191
	assert.Equal(t, int32(9191), EffectiveMetricsPort(inst))
}

func TestEffectiveAgentTag_AutoUpdateTargetPrecedence(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{}
	inst.Spec.Image.Tag = "stable"
	inst.Spec.AutoUpdate.Enabled = true
	inst.Status.AutoUpdate.CurrentTag = "v1.0.0"
	inst.Status.AutoUpdate.TargetTag = "v1.1.0"

	assert.Equal(t, "v1.1.0", EffectiveAgentTag(inst))
}

func TestEffectiveAgentTag_AutoUpdateCurrentFallback(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{}
	inst.Spec.Image.Tag = "stable"
	inst.Spec.AutoUpdate.Enabled = true
	inst.Status.AutoUpdate.CurrentTag = "v1.0.0"

	assert.Equal(t, "v1.0.0", EffectiveAgentTag(inst))
}

func TestEffectiveAgentTag_NonAutoUpdateUsesSpecTag(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{}
	inst.Spec.Image.Tag = "stable"
	inst.Status.AutoUpdate.CurrentTag = "v1.0.0"
	inst.Status.AutoUpdate.TargetTag = "v1.1.0"

	assert.Equal(t, "stable", EffectiveAgentTag(inst))
}
