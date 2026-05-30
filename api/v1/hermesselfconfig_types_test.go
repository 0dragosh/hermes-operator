package v1

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHermesSelfConfig_RootSerialises(t *testing.T) {
	t.Parallel()
	sc := &HermesSelfConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "install-skill", Namespace: "agents"},
		Spec: HermesSelfConfigSpec{
			InstanceRef: "my-hermes",
			AddSkills: []SelfConfigSkill{
				{Source: "git+https://github.com/foo/finance-skill@v1.2.0"},
			},
		},
	}
	assert.Equal(t, "my-hermes", sc.Spec.InstanceRef)
	assert.Len(t, sc.Spec.AddSkills, 1)
	assert.Equal(t, "git+https://github.com/foo/finance-skill@v1.2.0", sc.Spec.AddSkills[0].Source)
	_ = apiextensionsv1.JSON{} // keep import used (used in later tests)
}

func TestHermesSelfConfig_AllMutationFields(t *testing.T) {
	t.Parallel()
	sc := &HermesSelfConfig{
		Spec: HermesSelfConfigSpec{
			InstanceRef: "my-hermes",
			PatchConfig: &apiextensionsv1.JSON{
				Raw: []byte(`{"schedules":{"morning-brief":"0 8 * * *"}}`),
			},
			AddEnvVars: []SelfConfigEnvVar{
				{Name: "FINANCE_TZ", Value: Ptr("Europe/Berlin")},
			},
			AddWorkspaceFiles: []SelfConfigWorkspaceFile{
				{Path: "notes/finance.md", Content: Ptr("# Finance notes")},
			},
			AddProfileSnapshot: &SelfConfigProfileSnapshot{
				ProfileID: "user-42",
				Data:      "opaque-honcho-payload",
			},
		},
	}
	assert.NotNil(t, sc.Spec.PatchConfig)
	assert.JSONEq(t, `{"schedules":{"morning-brief":"0 8 * * *"}}`, string(sc.Spec.PatchConfig.Raw))
	require.NotNil(t, sc.Spec.AddEnvVars[0].Value)
	assert.Equal(t, "Europe/Berlin", *sc.Spec.AddEnvVars[0].Value)
	assert.Equal(t, "notes/finance.md", sc.Spec.AddWorkspaceFiles[0].Path)
	assert.Equal(t, "user-42", sc.Spec.AddProfileSnapshot.ProfileID)
}

func TestHermesSelfConfig_ProfileIDValidationMarkers(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("hermesselfconfig_types.go")
	require.NoError(t, err)

	assert.Contains(t, string(body), "// +kubebuilder:validation:MinLength=1\n\t// +kubebuilder:validation:MaxLength=253\n\t// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`\n\tProfileID string `json:\"profileID\"`")
}

func TestHermesSelfConfig_StatusShape(t *testing.T) {
	t.Parallel()
	now := metav1.Now()
	sc := &HermesSelfConfig{
		Status: HermesSelfConfigStatus{
			ObservedGeneration: 7,
			Phase:              SelfConfigPhaseApplied,
			AppliedAt:          &now,
			DenyReason:         "",
			AppliedFields: []string{
				"spec.env[name=FINANCE_TZ]",
				"spec.skills[source=git+https://github.com/foo/finance-skill@v1.2.0]",
			},
			Conditions: []metav1.Condition{{
				Type:               string(SelfConfigConditionApplied),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "SelfConfigApplied",
				Message:            "applied 2 fields",
			}},
		},
	}
	assert.Equal(t, SelfConfigPhaseApplied, sc.Status.Phase)
	assert.Len(t, sc.Status.AppliedFields, 2)
	assert.Equal(t, int64(7), sc.Status.ObservedGeneration)
}
