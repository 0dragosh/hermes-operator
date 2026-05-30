package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func TestAutoUpdateStartRolloutOnlyUpdatesStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := autoUpdateTestScheme(t)
	inst := autoUpdateTestInstance()
	sts := autoUpdateTestStatefulSet(inst, "ghcr.io/paperclipinc/hermes-agent:1.0.0")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1.HermesInstance{}).
		WithObjects(inst.DeepCopy(), sts.DeepCopy()).
		Build()
	reconciler := &AutoUpdateReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		Now:      func() time.Time { return time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC) },
	}
	liveInst := &hermesv1.HermesInstance{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: inst.Name, Namespace: inst.Namespace}, liveInst))

	result, err := reconciler.startRollout(ctx, liveInst, "1.1.0")

	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)

	liveSTS := &appsv1.StatefulSet{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: inst.Name, Namespace: inst.Namespace}, liveSTS))
	require.Len(t, liveSTS.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, "ghcr.io/paperclipinc/hermes-agent:1.0.0", liveSTS.Spec.Template.Spec.Containers[0].Image,
		"auto-update must let the main reconciler apply the target image from status")

	refetchedInst := &hermesv1.HermesInstance{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: inst.Name, Namespace: inst.Namespace}, refetchedInst))
	assert.Equal(t, "1.1.0", refetchedInst.Status.AutoUpdate.TargetTag)
	assert.NotNil(t, refetchedInst.Status.AutoUpdate.RolloutDeadline)
	assert.Equal(t, int32(0), refetchedInst.Status.AutoUpdate.ProbeFailures)
}

func TestAutoUpdateRollbackOnlyUpdatesStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := autoUpdateTestScheme(t)
	inst := autoUpdateTestInstance()
	inst.Status.AutoUpdate.TargetTag = "1.1.0"
	inst.Status.AutoUpdate.LastSuccessTag = "1.0.0"
	deadline := metav1.NewTime(time.Date(2026, 5, 29, 12, 5, 0, 0, time.UTC))
	inst.Status.AutoUpdate.RolloutDeadline = &deadline
	sts := autoUpdateTestStatefulSet(inst, "ghcr.io/paperclipinc/hermes-agent:1.1.0")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1.HermesInstance{}).
		WithObjects(inst.DeepCopy(), sts.DeepCopy()).
		Build()
	reconciler := &AutoUpdateReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}
	liveInst := &hermesv1.HermesInstance{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: inst.Name, Namespace: inst.Namespace}, liveInst))

	_, err := reconciler.rollback(ctx, liveInst, "1.1.0", "probe failures")

	require.NoError(t, err)

	liveSTS := &appsv1.StatefulSet{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: inst.Name, Namespace: inst.Namespace}, liveSTS))
	require.Len(t, liveSTS.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, "ghcr.io/paperclipinc/hermes-agent:1.1.0", liveSTS.Spec.Template.Spec.Containers[0].Image,
		"rollback must let the main reconciler apply the previous tag from status")

	refetchedInst := &hermesv1.HermesInstance{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: inst.Name, Namespace: inst.Namespace}, refetchedInst))
	assert.Equal(t, "1.0.0", refetchedInst.Status.AutoUpdate.CurrentTag)
	assert.Empty(t, refetchedInst.Status.AutoUpdate.TargetTag)
	assert.Equal(t, "1.1.0", refetchedInst.Status.AutoUpdate.LastFailedTag)
	assert.Nil(t, refetchedInst.Status.AutoUpdate.RolloutDeadline)
}

func autoUpdateTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, hermesv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	return scheme
}

func autoUpdateTestInstance() *hermesv1.HermesInstance {
	disableBackup := false
	return &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-autoupdate-unit", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Image: hermesv1.ImageSpec{
				Repository: "ghcr.io/paperclipinc/hermes-agent",
				Tag:        "1.0.0",
			},
			AutoUpdate: hermesv1.AutoUpdateSpec{
				Enabled:            true,
				BackupBeforeUpdate: &disableBackup,
			},
		},
	}
}

func autoUpdateTestStatefulSet(inst *hermesv1.HermesInstance, image string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: inst.Name, Namespace: inst.Namespace},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "hermes", Image: image}},
				},
			},
		},
	}
}
