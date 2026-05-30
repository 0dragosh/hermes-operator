package webhook

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func TestValidator_DenyEmptyImageRepository(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
		},
	}
	_, err := v.ValidateCreate(context.Background(), inst)
	assert.Error(t, err, "image.repository is required")
}

func TestValidator_DenyMissingImageTagForMutableReference(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent"},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
		},
	}

	_, err := v.ValidateCreate(context.Background(), inst)

	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "spec.image.tag is required")
	}
}

func TestValidator_DenyLatestImageTagForMutableReference(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent", Tag: "latest"},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
		},
	}

	_, err := v.ValidateCreate(context.Background(), inst)

	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "must not be \"latest\"")
	}
}

func TestValidator_AllowsDigestImageWithoutTag(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image: hermesv1.ImageSpec{
				Repository: "ghcr.io/paperclipinc/hermes-agent@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), inst)

	assert.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestValidator_DenyMalformedDigestImageRepository(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent@sha256:bad", Tag: "v1.0.0"},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
		},
	}

	_, err := v.ValidateCreate(context.Background(), inst)

	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "must use @sha256:<64 lowercase hex chars>")
	}
}

func TestValidator_DenyConfigRawAndConfigMapRefWithoutMergeMode(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "x", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			Config: hermesv1.ConfigSpec{
				Raw:          &hermesv1.RawConfig{RawExtension: runtime.RawExtension{Raw: []byte("{}")}},
				ConfigMapRef: &corev1.LocalObjectReference{Name: "x"},
				MergeMode:    "",
			},
		},
	}
	warns, err := v.ValidateCreate(context.Background(), inst)
	assert.NoError(t, err)
	assert.NotEmpty(t, warns)
}

func TestValidator_DenySelfConfigureEnabledNoProtectedKeys(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:         hermesv1.ImageSpec{Repository: "x", Tag: hermesv1.DefaultAgentImageTag},
			Storage:       hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			SelfConfigure: hermesv1.SelfConfigureSpec{Enabled: Ptr(true), AllowedActions: []hermesv1.SelfConfigAction{hermesv1.ActionSkills}},
		},
	}
	_, err := v.ValidateCreate(context.Background(), inst)
	assert.Error(t, err)
}

func TestValidator_DenyImmutableStorageClassName(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	old := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Image: hermesv1.ImageSpec{Repository: "x", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{
				Persistence: hermesv1.PersistenceSpec{Size: "1Gi", StorageClassName: Ptr("gp3")},
			},
		},
	}
	newer := old.DeepCopy()
	newer.Spec.Storage.Persistence.StorageClassName = Ptr("io2")

	_, err := v.ValidateUpdate(context.Background(), old, newer)
	assert.Error(t, err)
}

func TestValidator_DenyBothPDBValuesSet(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	mi := intOrStr("50%")
	mu := intOrStr("1")
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "x", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			Availability: hermesv1.AvailabilitySpec{
				PodDisruptionBudget: hermesv1.PDBSpec{Enabled: Ptr(true), MinAvailable: &mi, MaxUnavailable: &mu},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), inst)
	assert.Error(t, err)
}

func TestValidator_AllowHappyPath(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	inst := validHermesInstance()
	warns, err := v.ValidateCreate(context.Background(), inst)
	assert.NoError(t, err)
	assert.Empty(t, warns)
}

func TestValidator_RejectsUnsafeWorkspacePaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*hermesv1.HermesInstance)
		want   string
	}{
		{
			name: "initial file with configmap-invalid character",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Workspace.InitialFiles = []hermesv1.WorkspaceFile{{Path: "notes/foo bar.md", Content: "x"}}
			},
			want: "letters, digits",
		},
		{
			name: "initial file with traversal",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Workspace.InitialFiles = []hermesv1.WorkspaceFile{{Path: "notes/../secret.md", Content: "x"}}
			},
			want: "dot segments",
		},
		{
			name: "initial dir with absolute path",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Workspace.InitialDirs = []string{"/etc"}
			},
			want: "relative",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := &HermesInstanceValidator{}
			inst := validHermesInstance()
			tc.mutate(inst)

			_, err := v.ValidateCreate(context.Background(), inst)

			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

func TestValidator_AllowsWorkspacePathsWithUnderscores(t *testing.T) {
	t.Parallel()
	v := &HermesInstanceValidator{}
	inst := validHermesInstance()
	inst.Spec.Workspace.InitialFiles = []hermesv1.WorkspaceFile{
		{Path: "notes/foo__bar.md", Content: "x"},
		{Path: "notes/a_/b.md", Content: "x"},
		{Path: "notes/a/_b.md", Content: "x"},
	}

	_, err := v.ValidateCreate(context.Background(), inst)

	assert.NoError(t, err)
}

func validHermesInstance() *hermesv1.HermesInstance {
	return &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
		},
	}
}

func newValidatorWithObjs(t *testing.T, objs ...client.Object) *HermesInstanceValidator {
	t.Helper()
	scheme := newScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &HermesInstanceValidator{Client: c}
}

func TestValidateGateways_TelegramSecretMissingProducesWarning(t *testing.T) {
	t.Parallel()
	v := newValidatorWithObjs(t)
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "x", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			Gateways: hermesv1.GatewaysSpec{
				Telegram: hermesv1.TelegramGatewaySpec{
					Enabled: Ptr(true),
					BotTokenSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "missing"},
						Key:                  "token",
					},
				},
			},
		},
	}
	warnings, err := v.ValidateCreate(context.Background(), inst)
	assert.NoError(t, err)
	assert.NotEmpty(t, warnings)
}

func TestValidateGateways_TelegramEnabledWithoutSecretRefDenied(t *testing.T) {
	t.Parallel()
	v := newValidatorWithObjs(t)
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "x", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			Gateways: hermesv1.GatewaysSpec{
				Telegram: hermesv1.TelegramGatewaySpec{Enabled: Ptr(true)},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), inst)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "botTokenSecretRef")
}

func TestValidateGateways_SecretExistsNoWarning(t *testing.T) {
	t.Parallel()
	v := newValidatorWithObjs(t, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tg", Namespace: "agents"},
		Data:       map[string][]byte{"token": []byte("x")},
	})
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "x", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			Gateways: hermesv1.GatewaysSpec{
				Telegram: hermesv1.TelegramGatewaySpec{
					Enabled: Ptr(true),
					BotTokenSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "tg"},
						Key:                  "token",
					},
				},
			},
		},
	}
	warnings, err := v.ValidateCreate(context.Background(), inst)
	assert.NoError(t, err)
	for _, w := range warnings {
		assert.NotContains(t, w, "gateways.telegram")
	}
}

func TestValidateSelfConfigure_ProfilesActionAllowed(t *testing.T) {
	t.Parallel()
	v := newValidatorWithObjs(t)
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "x", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			SelfConfigure: hermesv1.SelfConfigureSpec{
				Enabled:        Ptr(true),
				AllowedActions: []hermesv1.SelfConfigAction{hermesv1.ActionProfiles},
				ProtectedKeys:  []string{"provider.apiKey"},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), inst)
	assert.NoError(t, err)
}

func TestValidateSelfConfigure_UnknownActionDenied(t *testing.T) {
	t.Parallel()
	v := newValidatorWithObjs(t)
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "x", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			SelfConfigure: hermesv1.SelfConfigureSpec{
				Enabled:        Ptr(true),
				AllowedActions: []hermesv1.SelfConfigAction{"reboot-cluster"},
				ProtectedKeys:  []string{"provider.apiKey"},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), inst)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reboot-cluster")
}

func TestValidateRestoreFromImmutableAfterLatch(t *testing.T) {
	old := &hermesv1.HermesInstance{
		Spec:   hermesv1.HermesInstanceSpec{RestoreFrom: "k1"},
		Status: hermesv1.HermesInstanceStatus{RestoredFrom: "k1"},
	}
	newer := old.DeepCopy()
	newer.Spec.RestoreFrom = "k2"
	errs := validateImmutableTerminals(old, newer)
	assert.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "spec.restoreFrom")
}

func TestValidateMigrationImmutableAfterCompleted(t *testing.T) {
	old := &hermesv1.HermesInstance{
		Spec: hermesv1.HermesInstanceSpec{
			Migration: hermesv1.MigrationSpec{
				FromOpenClaw: &hermesv1.MigrationFromOpenClawSpec{
					Mode: "copy",
					Source: hermesv1.MigrationFromOpenClawSource{
						OpenClawInstanceRef: &hermesv1.NamespacedObjectReference{Name: "x", Namespace: "y"},
					},
				},
			},
		},
		Status: hermesv1.HermesInstanceStatus{Migration: hermesv1.MigrationStatus{Completed: true}},
	}
	newer := old.DeepCopy()
	newer.Spec.Migration.FromOpenClaw.Mode = "move"
	errs := validateImmutableTerminals(old, newer)
	assert.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "migration")
}

func TestValidateMutualExclusion(t *testing.T) {
	inst := &hermesv1.HermesInstance{
		Spec: hermesv1.HermesInstanceSpec{
			RestoreFrom: "k1",
			Migration: hermesv1.MigrationSpec{
				FromOpenClaw: &hermesv1.MigrationFromOpenClawSpec{
					Source: hermesv1.MigrationFromOpenClawSource{
						OpenClawInstanceRef: &hermesv1.NamespacedObjectReference{Name: "x", Namespace: "y"},
					},
				},
			},
		},
	}
	errs := validateRestoreMigrationMutualExclusion(inst)
	assert.NotEmpty(t, errs)
}

func TestValidateMigrationSourceExactlyOne(t *testing.T) {
	both := &hermesv1.HermesInstance{
		Spec: hermesv1.HermesInstanceSpec{
			Migration: hermesv1.MigrationSpec{
				FromOpenClaw: &hermesv1.MigrationFromOpenClawSpec{
					Source: hermesv1.MigrationFromOpenClawSource{
						OpenClawInstanceRef: &hermesv1.NamespacedObjectReference{Name: "x", Namespace: "y"},
						BackupRef:           &hermesv1.MigrationBackupRef{S3: hermesv1.MigrationBackupS3{Bucket: "b", Key: "k", Endpoint: "e", CredentialsSecretRef: hermesv1.LocalObjectReference{Name: "s"}}},
					},
				},
			},
		},
	}
	assert.NotEmpty(t, validateMigrationSourceExactlyOne(both))

	neither := &hermesv1.HermesInstance{
		Spec: hermesv1.HermesInstanceSpec{
			Migration: hermesv1.MigrationSpec{
				FromOpenClaw: &hermesv1.MigrationFromOpenClawSpec{
					Source: hermesv1.MigrationFromOpenClawSource{},
				},
			},
		},
	}
	assert.NotEmpty(t, validateMigrationSourceExactlyOne(neither))

	one := &hermesv1.HermesInstance{
		Spec: hermesv1.HermesInstanceSpec{
			Migration: hermesv1.MigrationSpec{
				FromOpenClaw: &hermesv1.MigrationFromOpenClawSpec{
					Source: hermesv1.MigrationFromOpenClawSource{
						OpenClawInstanceRef: &hermesv1.NamespacedObjectReference{Name: "x", Namespace: "y"},
					},
				},
			},
		},
	}
	assert.Empty(t, validateMigrationSourceExactlyOne(one))
}

func TestValidateRejectsUnsupportedExtraAptPackages(t *testing.T) {
	t.Parallel()

	inst := validHermesInstance()
	inst.Spec.Runtime.ExtraAptPackages = []string{"curl"}

	_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "custom agent image")
	}
}

func TestValidateRejectsUnsafeContainerSecurity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*hermesv1.HermesInstance)
		want   string
	}{
		{
			name: "agent privileged",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.ContainerSecurityContext = &corev1.SecurityContext{Privileged: Ptr(true)}
			},
			want: "privileged",
		},
		{
			name: "agent allows privilege escalation",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.ContainerSecurityContext = &corev1.SecurityContext{AllowPrivilegeEscalation: Ptr(true)}
			},
			want: "allowPrivilegeEscalation",
		},
		{
			name: "agent runs as root",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.ContainerSecurityContext = &corev1.SecurityContext{RunAsUser: Ptr[int64](0)}
			},
			want: "runAsUser",
		},
		{
			name: "agent adds capabilities",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.ContainerSecurityContext = &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"NET_ADMIN"}},
				}
			},
			want: "capabilities",
		},
		{
			name: "pod runs as root",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.PodSecurityContext = &corev1.PodSecurityContext{RunAsUser: Ptr[int64](0)}
			},
			want: "runAsUser",
		},
		{
			name: "init container privileged",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.InitContainers = []corev1.Container{{
					Name:            "setup",
					Image:           "busybox",
					SecurityContext: &corev1.SecurityContext{Privileged: Ptr(true)},
				}}
			},
			want: "initContainers",
		},
		{
			name: "sidecar adds capabilities",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Sidecars = []corev1.Container{{
					Name:  "debug",
					Image: "busybox",
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"SYS_ADMIN"}},
					},
				}}
			},
			want: "sidecars",
		},
		{
			name: "agent disables runAsNonRoot",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.ContainerSecurityContext = &corev1.SecurityContext{RunAsNonRoot: Ptr(false)}
			},
			want: "runAsNonRoot",
		},
		{
			name: "agent disables read-only root filesystem",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.ContainerSecurityContext = &corev1.SecurityContext{ReadOnlyRootFilesystem: Ptr(false)}
			},
			want: "readOnlyRootFilesystem",
		},
		{
			name: "agent drops only a subset of capabilities",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.ContainerSecurityContext = &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"NET_RAW"}},
				}
			},
			want: "capabilities.drop",
		},
		{
			name: "agent uses unconfined seccomp",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.ContainerSecurityContext = &corev1.SecurityContext{
					SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined},
				}
			},
			want: "seccompProfile",
		},
		{
			name: "pod disables runAsNonRoot",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.PodSecurityContext = &corev1.PodSecurityContext{RunAsNonRoot: Ptr(false)}
			},
			want: "runAsNonRoot",
		},
		{
			name: "pod uses unconfined seccomp",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Security.PodSecurityContext = &corev1.PodSecurityContext{
					SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined},
				}
			},
			want: "seccompProfile",
		},
		{
			name: "init container host port",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.InitContainers = []corev1.Container{{
					Name:  "setup",
					Image: "busybox",
					Ports: []corev1.ContainerPort{{
						ContainerPort: 8080,
						HostPort:      30080,
					}},
				}}
			},
			want: "hostPort",
		},
		{
			name: "sidecar host port",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Sidecars = []corev1.Container{{
					Name:  "debug",
					Image: "busybox",
					Ports: []corev1.ContainerPort{{
						ContainerPort: 8080,
						HostPort:      30080,
					}},
				}}
			},
			want: "hostPort",
		},
		{
			name: "init container host IP",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.InitContainers = []corev1.Container{{
					Name:  "setup",
					Image: "busybox",
					Ports: []corev1.ContainerPort{{
						ContainerPort: 8080,
						HostIP:        "0.0.0.0",
					}},
				}}
			},
			want: "hostIP",
		},
		{
			name: "sidecar host IP",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Sidecars = []corev1.Container{{
					Name:  "debug",
					Image: "busybox",
					Ports: []corev1.ContainerPort{{
						ContainerPort: 8080,
						HostIP:        "0.0.0.0",
					}},
				}}
			},
			want: "hostIP",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst := validHermesInstance()
			tc.mutate(inst)

			_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

func TestValidateRejectsUnsafeVolumesAndMounts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*hermesv1.HermesInstance)
		want   string
	}{
		{
			name: "hostPath volume",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.ExtraVolumes = []corev1.Volume{{
					Name:         "host",
					VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kubelet"}},
				}}
			},
			want: "hostPath",
		},
		{
			name: "projected service account token",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.ExtraVolumes = []corev1.Volume{{
					Name: "token",
					VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token"},
					}}}},
				}}
			},
			want: "serviceAccountToken",
		},
		{
			name: "agent mount at root",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.ExtraVolumeMounts = []corev1.VolumeMount{{Name: "host", MountPath: "/"}}
			},
			want: "mountPath",
		},
		{
			name: "init container proc mount",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.InitContainers = []corev1.Container{{
					Name:         "setup",
					Image:        "busybox",
					VolumeMounts: []corev1.VolumeMount{{Name: "proc", MountPath: "/proc"}},
				}}
			},
			want: "mountPath",
		},
		{
			name: "sidecar docker socket mount",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Sidecars = []corev1.Container{{
					Name:         "debug",
					Image:        "busybox",
					VolumeMounts: []corev1.VolumeMount{{Name: "docker", MountPath: "/var/run/docker.sock"}},
				}}
			},
			want: "docker.sock",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst := validHermesInstance()
			tc.mutate(inst)

			_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

func TestValidateRejectsUnsafeNetworking(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*hermesv1.HermesInstance)
		want   string
	}{
		{
			name: "NodePort service",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Networking.Service.Type = corev1.ServiceTypeNodePort
			},
			want: "NodePort",
		},
		{
			name: "LoadBalancer service",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Networking.Service.Type = corev1.ServiceTypeLoadBalancer
			},
			want: "LoadBalancer",
		},
		{
			name: "ingress without TLS",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Networking.Ingress.Enabled = Ptr(true)
			},
			want: "TLS",
		},
		{
			name: "ingress disables TLS redirect",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Networking.Ingress.Enabled = Ptr(true)
				inst.Spec.Networking.Ingress.TLS = []hermesv1.IngressTLSSpec{{SecretName: "hermes-tls"}}
				inst.Spec.Networking.Ingress.Annotations = map[string]string{"nginx.ingress.kubernetes.io/ssl-redirect": "false"}
			},
			want: "ssl-redirect",
		},
		{
			name: "ingress disables global auth",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Networking.Ingress.Enabled = Ptr(true)
				inst.Spec.Networking.Ingress.TLS = []hermesv1.IngressTLSSpec{{SecretName: "hermes-tls"}}
				inst.Spec.Networking.Ingress.Annotations = map[string]string{"nginx.ingress.kubernetes.io/enable-global-auth": "false"}
			},
			want: "enable-global-auth",
		},
		{
			name: "service requests public load balancer",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Networking.Service.Annotations = map[string]string{"service.beta.kubernetes.io/aws-load-balancer-scheme": "internet-facing"}
			},
			want: "internet-facing",
		},
		{
			name: "metrics enabled without secure transport",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Observability.Metrics.Enabled = Ptr(true)
				inst.Spec.Observability.Metrics.Secure = Ptr(false)
			},
			want: "secure",
		},
		{
			name: "metrics default enabled with insecure transport",
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Observability.Metrics.Secure = Ptr(false)
			},
			want: "secure",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst := validHermesInstance()
			tc.mutate(inst)

			_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

func TestValidateAllowsSafeNetworkingAndSecurity(t *testing.T) {
	t.Parallel()

	inst := validHermesInstance()
	inst.Spec.Security.PodSecurityContext = &corev1.PodSecurityContext{RunAsUser: Ptr[int64](1000)}
	inst.Spec.Security.ContainerSecurityContext = &corev1.SecurityContext{
		RunAsUser:                Ptr[int64](1000),
		AllowPrivilegeEscalation: Ptr(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
	inst.Spec.Networking.Service.Type = corev1.ServiceTypeClusterIP
	inst.Spec.Networking.Ingress.Enabled = Ptr(true)
	inst.Spec.Networking.Ingress.TLS = []hermesv1.IngressTLSSpec{{SecretName: "hermes-tls", Hosts: []string{"hermes.example.com"}}}
	inst.Spec.Networking.Ingress.PathType = networkingv1.PathTypePrefix
	inst.Spec.Observability.Metrics.Enabled = Ptr(true)
	inst.Spec.Observability.Metrics.Secure = Ptr(true)

	_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
	assert.NoError(t, err)
}

func TestValidateAllowsMetricsEnabledWithSecureOmitted(t *testing.T) {
	t.Parallel()

	inst := validHermesInstance()
	inst.Spec.Observability.Metrics.Enabled = Ptr(true)

	_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
	assert.NoError(t, err)
}

func validMigrationS3Instance() *hermesv1.HermesInstance {
	return &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			Migration: hermesv1.MigrationSpec{
				FromOpenClaw: &hermesv1.MigrationFromOpenClawSpec{
					Mode: "copy",
					Source: hermesv1.MigrationFromOpenClawSource{
						BackupRef: &hermesv1.MigrationBackupRef{
							S3: hermesv1.MigrationBackupS3{
								Bucket:               "openclaw-backups",
								Endpoint:             "s3.amazonaws.com",
								Key:                  "prod/my-openclaw/2026-05-11.tar.zst",
								CredentialsSecretRef: hermesv1.LocalObjectReference{Name: "oc-s3-creds"},
							},
						},
					},
				},
			},
		},
	}
}

func validBackupS3Instance() *hermesv1.HermesInstance {
	return &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image:   hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent", Tag: hermesv1.DefaultAgentImageTag},
			Storage: hermesv1.StorageSpec{Persistence: hermesv1.PersistenceSpec{Size: "1Gi"}},
			Backup: hermesv1.BackupSpec{
				S3: &hermesv1.BackupS3Spec{
					Bucket:               "hermes-backups",
					Endpoint:             "s3.amazonaws.com",
					PathPrefix:           "prod/hermes",
					CredentialsSecretRef: hermesv1.LocalObjectReference{Name: "s3-creds"},
				},
			},
		},
	}
}

func TestValidateS3MigrationBackupRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*hermesv1.MigrationBackupS3)
		want   string
	}{
		{name: "empty bucket", mutate: func(s3 *hermesv1.MigrationBackupS3) { s3.Bucket = "" }, want: "bucket"},
		{name: "key traversal", mutate: func(s3 *hermesv1.MigrationBackupS3) { s3.Key = "../backup.tar.zst" }, want: "key"},
		{name: "key shell metacharacter", mutate: func(s3 *hermesv1.MigrationBackupS3) { s3.Key = "prod/backup;rm.tar.zst" }, want: "key"},
		{name: "key control character", mutate: func(s3 *hermesv1.MigrationBackupS3) { s3.Key = "prod/backup\nname.tar.zst" }, want: "key"},
		{name: "endpoint whitespace", mutate: func(s3 *hermesv1.MigrationBackupS3) { s3.Endpoint = "s3.amazonaws.com bad" }, want: "endpoint"},
		{name: "endpoint shell metacharacter", mutate: func(s3 *hermesv1.MigrationBackupS3) { s3.Endpoint = "s3.amazonaws.com;rm" }, want: "endpoint"},
		{name: "endpoint traversal", mutate: func(s3 *hermesv1.MigrationBackupS3) { s3.Endpoint = "https://s3.amazonaws.com/../admin" }, want: "endpoint"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst := validMigrationS3Instance()
			tc.mutate(&inst.Spec.Migration.FromOpenClaw.Source.BackupRef.S3)
			_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

func TestValidateS3BackupRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*hermesv1.BackupS3Spec)
		want   string
	}{
		{name: "empty bucket", mutate: func(s3 *hermesv1.BackupS3Spec) { s3.Bucket = "" }, want: "bucket"},
		{name: "path prefix traversal", mutate: func(s3 *hermesv1.BackupS3Spec) { s3.PathPrefix = "prod/../snapshots" }, want: "pathPrefix"},
		{name: "path prefix shell metacharacter", mutate: func(s3 *hermesv1.BackupS3Spec) { s3.PathPrefix = "prod;snapshots" }, want: "pathPrefix"},
		{name: "endpoint whitespace", mutate: func(s3 *hermesv1.BackupS3Spec) { s3.Endpoint = "s3.amazonaws.com bad" }, want: "endpoint"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst := validBackupS3Instance()
			tc.mutate(inst.Spec.Backup.S3)
			_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

func TestValidateS3BackupAcceptsEmptyPathPrefix(t *testing.T) {
	t.Parallel()
	inst := validBackupS3Instance()
	inst.Spec.Backup.S3.PathPrefix = ""

	_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
	assert.NoError(t, err)
}

func TestValidateS3AcceptsCommonObjectKeyCharacters(t *testing.T) {
	t.Parallel()
	inst := validMigrationS3Instance()
	inst.Spec.Migration.FromOpenClaw.Source.BackupRef.S3.Key = "prod/team_1/openclaw.backup-2026-05-11T00:00:00Z.tar.zst"

	_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), inst)
	assert.NoError(t, err)
}

func TestValidateS3RejectsLengthOverflow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		inst   *hermesv1.HermesInstance
		mutate func(*hermesv1.HermesInstance)
		want   string
	}{
		{
			name: "backup endpoint too long",
			inst: validBackupS3Instance(),
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Backup.S3.Endpoint = "s3." + strings.Repeat("a", 251)
			},
			want: "endpoint",
		},
		{
			name: "backup path prefix too long",
			inst: validBackupS3Instance(),
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Backup.S3.PathPrefix = strings.Repeat("a", 1025)
			},
			want: "pathPrefix",
		},
		{
			name: "migration key too long",
			inst: validMigrationS3Instance(),
			mutate: func(inst *hermesv1.HermesInstance) {
				inst.Spec.Migration.FromOpenClaw.Source.BackupRef.S3.Key = strings.Repeat("a", 1025)
			},
			want: "key",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.mutate(tc.inst)
			_, err := (&HermesInstanceValidator{}).ValidateCreate(context.Background(), tc.inst)
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}
