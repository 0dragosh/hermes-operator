package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func newSelfConfigValidator(t *testing.T, objs ...client.Object) *HermesSelfConfigValidator {
	t.Helper()
	s := newScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
	return &HermesSelfConfigValidator{Client: c}
}

func selfConfigParent(name string, profileEnabled bool) *hermesv1.HermesInstance {
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
	}
	if profileEnabled {
		t := true
		inst.Spec.ProfileStore.Honcho.Enabled = &t
	}
	return inst
}

func TestSCValidate_RejectsMissingInstance(t *testing.T) {
	t.Parallel()
	v := newSelfConfigValidator(t)
	sc := &hermesv1.HermesSelfConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
		Spec:       hermesv1.HermesSelfConfigSpec{InstanceRef: "nope"},
	}
	_, err := v.ValidateCreate(context.Background(), sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instanceRef")
}

func TestSCValidate_RejectsEmptyInstanceRef(t *testing.T) {
	t.Parallel()
	v := newSelfConfigValidator(t)
	sc := &hermesv1.HermesSelfConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
	}
	_, err := v.ValidateCreate(context.Background(), sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instanceRef")
}

func TestSCValidate_AcceptsValidRequest(t *testing.T) {
	t.Parallel()
	v := newSelfConfigValidator(t, selfConfigParent("my-hermes", false))
	sc := &hermesv1.HermesSelfConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
		Spec: hermesv1.HermesSelfConfigSpec{
			InstanceRef: "my-hermes",
			AddSkills:   []hermesv1.SelfConfigSkill{{Source: "git+x"}},
		},
	}
	warns, err := v.ValidateCreate(context.Background(), sc)
	require.NoError(t, err)
	assert.Empty(t, warns)
}

func TestSCValidate_RejectsInvalidEnvVarValueSources(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		env  hermesv1.SelfConfigEnvVar
		want string
	}{
		{
			name: "value and valueFrom both set",
			env: hermesv1.SelfConfigEnvVar{
				Name:  "TOKEN",
				Value: Ptr("literal"),
				ValueFrom: &hermesv1.SelfConfigEnvVarSource{
					SecretKeyRef: &hermesv1.SelfConfigKeySelector{Name: "tokens", Key: "token"},
				},
			},
			want: "value",
		},
		{
			name: "empty value and valueFrom both set",
			env: hermesv1.SelfConfigEnvVar{
				Name:  "TOKEN",
				Value: Ptr(""),
				ValueFrom: &hermesv1.SelfConfigEnvVarSource{
					SecretKeyRef: &hermesv1.SelfConfigKeySelector{Name: "tokens", Key: "token"},
				},
			},
			want: "value",
		},
		{
			name: "neither value nor valueFrom set",
			env:  hermesv1.SelfConfigEnvVar{Name: "TOKEN"},
			want: "value",
		},
		{
			name: "secretKeyRef and configMapKeyRef both set",
			env: hermesv1.SelfConfigEnvVar{
				Name: "TOKEN",
				ValueFrom: &hermesv1.SelfConfigEnvVarSource{
					SecretKeyRef:    &hermesv1.SelfConfigKeySelector{Name: "tokens", Key: "token"},
					ConfigMapKeyRef: &hermesv1.SelfConfigKeySelector{Name: "tokens", Key: "token"},
				},
			},
			want: "secretKeyRef",
		},
		{
			name: "valueFrom without source",
			env: hermesv1.SelfConfigEnvVar{
				Name:      "TOKEN",
				ValueFrom: &hermesv1.SelfConfigEnvVarSource{},
			},
			want: "valueFrom",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := newSelfConfigValidator(t, selfConfigParent("my-hermes", false))
			sc := &hermesv1.HermesSelfConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
				Spec: hermesv1.HermesSelfConfigSpec{
					InstanceRef: "my-hermes",
					AddEnvVars:  []hermesv1.SelfConfigEnvVar{tc.env},
				},
			}

			_, err := v.ValidateCreate(context.Background(), sc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestSCValidate_AcceptsEnvVarValueSources(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		env  hermesv1.SelfConfigEnvVar
	}{
		{name: "literal value", env: hermesv1.SelfConfigEnvVar{Name: "TOKEN", Value: Ptr("literal")}},
		{name: "empty literal value", env: hermesv1.SelfConfigEnvVar{Name: "TOKEN", Value: Ptr("")}},
		{
			name: "secret valueFrom",
			env: hermesv1.SelfConfigEnvVar{
				Name: "TOKEN",
				ValueFrom: &hermesv1.SelfConfigEnvVarSource{
					SecretKeyRef: &hermesv1.SelfConfigKeySelector{Name: "tokens", Key: "token"},
				},
			},
		},
		{
			name: "configMap valueFrom",
			env: hermesv1.SelfConfigEnvVar{
				Name: "TOKEN",
				ValueFrom: &hermesv1.SelfConfigEnvVarSource{
					ConfigMapKeyRef: &hermesv1.SelfConfigKeySelector{Name: "tokens", Key: "token"},
				},
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := newSelfConfigValidator(t, selfConfigParent("my-hermes", false))
			sc := &hermesv1.HermesSelfConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
				Spec: hermesv1.HermesSelfConfigSpec{
					InstanceRef: "my-hermes",
					AddEnvVars:  []hermesv1.SelfConfigEnvVar{tc.env},
				},
			}

			_, err := v.ValidateCreate(context.Background(), sc)
			require.NoError(t, err)
		})
	}
}

func TestSCValidate_RejectsInvalidWorkspaceFileSources(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		file hermesv1.SelfConfigWorkspaceFile
		want string
	}{
		{
			name: "content and contentFrom both set",
			file: hermesv1.SelfConfigWorkspaceFile{
				Path:        "notes/secure.md",
				Content:     Ptr("literal"),
				ContentFrom: &hermesv1.SelfConfigKeySelector{Name: "workspace", Key: "secure.md"},
			},
			want: "content",
		},
		{
			name: "empty content and contentFrom both set",
			file: hermesv1.SelfConfigWorkspaceFile{
				Path:        "notes/secure.md",
				Content:     Ptr(""),
				ContentFrom: &hermesv1.SelfConfigKeySelector{Name: "workspace", Key: "secure.md"},
			},
			want: "content",
		},
		{
			name: "neither content nor contentFrom set",
			file: hermesv1.SelfConfigWorkspaceFile{Path: "notes/secure.md"},
			want: "content",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := newSelfConfigValidator(t, selfConfigParent("my-hermes", false))
			sc := &hermesv1.HermesSelfConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
				Spec: hermesv1.HermesSelfConfigSpec{
					InstanceRef:       "my-hermes",
					AddWorkspaceFiles: []hermesv1.SelfConfigWorkspaceFile{tc.file},
				},
			}

			_, err := v.ValidateCreate(context.Background(), sc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestSCValidate_RejectsUnsafeWorkspacePaths(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{name: "space", path: "notes/foo bar.md", want: "letters, digits"},
		{name: "traversal", path: "notes/../secret.md", want: "dot segments"},
		{name: "absolute", path: "/secret.md", want: "relative"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := newSelfConfigValidator(t, selfConfigParent("my-hermes", false))
			sc := &hermesv1.HermesSelfConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
				Spec: hermesv1.HermesSelfConfigSpec{
					InstanceRef: "my-hermes",
					AddWorkspaceFiles: []hermesv1.SelfConfigWorkspaceFile{{
						Path:    tc.path,
						Content: Ptr("literal"),
					}},
				},
			}

			_, err := v.ValidateCreate(context.Background(), sc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestSCValidate_AcceptsWorkspaceFileSources(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		file hermesv1.SelfConfigWorkspaceFile
	}{
		{name: "literal content", file: hermesv1.SelfConfigWorkspaceFile{Path: "notes/secure.md", Content: Ptr("literal")}},
		{name: "empty literal content", file: hermesv1.SelfConfigWorkspaceFile{Path: "notes/secure.md", Content: Ptr("")}},
		{name: "literal content with underscores", file: hermesv1.SelfConfigWorkspaceFile{Path: "notes/foo__bar.md", Content: Ptr("literal")}},
		{
			name: "contentFrom",
			file: hermesv1.SelfConfigWorkspaceFile{
				Path:        "notes/secure.md",
				ContentFrom: &hermesv1.SelfConfigKeySelector{Name: "workspace", Key: "secure.md"},
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := newSelfConfigValidator(t, selfConfigParent("my-hermes", false))
			sc := &hermesv1.HermesSelfConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
				Spec: hermesv1.HermesSelfConfigSpec{
					InstanceRef:       "my-hermes",
					AddWorkspaceFiles: []hermesv1.SelfConfigWorkspaceFile{tc.file},
				},
			}

			_, err := v.ValidateCreate(context.Background(), sc)
			require.NoError(t, err)
		})
	}
}

func TestSCValidate_WarnsOnMultipleMutations(t *testing.T) {
	t.Parallel()
	v := newSelfConfigValidator(t, selfConfigParent("my-hermes", false))
	sc := &hermesv1.HermesSelfConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
		Spec: hermesv1.HermesSelfConfigSpec{
			InstanceRef: "my-hermes",
			AddSkills:   []hermesv1.SelfConfigSkill{{Source: "git+x"}},
			AddEnvVars:  []hermesv1.SelfConfigEnvVar{{Name: "X", Value: Ptr("y")}},
		},
	}
	warns, err := v.ValidateCreate(context.Background(), sc)
	require.NoError(t, err)
	require.NotEmpty(t, warns, "must warn: not deny: on multiple mutation fields")
	assert.Contains(t, warns[0], "atomic")
}

func TestSCValidate_RejectsInvalidJSONPatch(t *testing.T) {
	t.Parallel()
	v := newSelfConfigValidator(t, selfConfigParent("my-hermes", false))
	sc := &hermesv1.HermesSelfConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
		Spec: hermesv1.HermesSelfConfigSpec{
			InstanceRef: "my-hermes",
			PatchConfig: &apiextensionsv1.JSON{Raw: []byte(`{not-json`)},
		},
	}
	_, err := v.ValidateCreate(context.Background(), sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patchConfig")
}

func TestSCValidate_RejectsSnapshotWithoutHoncho(t *testing.T) {
	t.Parallel()
	v := newSelfConfigValidator(t, selfConfigParent("my-hermes", false))
	sc := &hermesv1.HermesSelfConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
		Spec: hermesv1.HermesSelfConfigSpec{
			InstanceRef: "my-hermes",
			AddProfileSnapshot: &hermesv1.SelfConfigProfileSnapshot{
				ProfileID: "u", Data: "d",
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "honcho")
}

func TestSCValidate_AcceptsSnapshotWithHoncho(t *testing.T) {
	t.Parallel()
	v := newSelfConfigValidator(t, selfConfigParent("my-hermes", true))
	sc := &hermesv1.HermesSelfConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
		Spec: hermesv1.HermesSelfConfigSpec{
			InstanceRef: "my-hermes",
			AddProfileSnapshot: &hermesv1.SelfConfigProfileSnapshot{
				ProfileID: "u", Data: "d",
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), sc)
	require.NoError(t, err)
}

func TestSelfConfigProfileIDPattern(t *testing.T) {
	t.Parallel()

	assert.False(t, selfConfigProfileIDPattern.MatchString("prod; touch /tmp/pwned"))
	assert.False(t, selfConfigProfileIDPattern.MatchString("../prod"))
	assert.True(t, selfConfigProfileIDPattern.MatchString("prod-user-1"))
}

func TestSCValidate_ProfileIDPattern(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		profileID string
		wantErr   bool
	}{
		{name: "rejects shell metacharacters", profileID: "prod; touch /tmp/pwned", wantErr: true},
		{name: "rejects path traversal", profileID: "../prod", wantErr: true},
		{name: "accepts hyphenated lowercase ID", profileID: "prod-user-1", wantErr: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := newSelfConfigValidator(t, selfConfigParent("my-hermes", true))
			sc := &hermesv1.HermesSelfConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
				Spec: hermesv1.HermesSelfConfigSpec{
					InstanceRef: "my-hermes",
					AddProfileSnapshot: &hermesv1.SelfConfigProfileSnapshot{
						ProfileID: tc.profileID,
						Data:      "d",
					},
				},
			}

			_, err := v.ValidateCreate(context.Background(), sc)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "profileID")
				return
			}
			require.NoError(t, err)
		})
	}
}
