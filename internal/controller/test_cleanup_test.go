package controller

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
	"github.com/paperclipinc/hermes-operator/internal/resources"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func cleanupHermesInstanceOwnedResources(ctx context.Context, name, namespace string) {
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	waitObjects := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: resources.StatefulSetName(inst), Namespace: namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: resources.HonchoDeploymentName(inst), Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: resources.ServiceName(inst), Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: resources.HonchoServiceName(inst), Namespace: namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: resources.ConfigMapName(inst), Namespace: namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: resources.WorkspaceConfigMapName(inst), Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: resources.GatewayTokenSecretName(inst), Namespace: namespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: resources.ServiceAccountName(inst), Namespace: namespace}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: resources.RoleName(inst), Namespace: namespace}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: resources.RoleBindingName(inst), Namespace: namespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: resources.NetworkPolicyName(inst), Namespace: namespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: resources.HonchoDeploymentName(inst), Namespace: namespace}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: resources.IngressName(inst), Namespace: namespace}},
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: resources.PDBName(inst), Namespace: namespace}},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: resources.HPAName(inst), Namespace: namespace}},
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: resources.BackupCronJobName(inst), Namespace: namespace}},
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: resources.BackupPruneCronJobName(inst), Namespace: namespace}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: FinalBackupJobName(inst), Namespace: namespace}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: PreUpdateBackupJobName(inst), Namespace: namespace}},
	}
	for _, obj := range waitObjects {
		_ = k8sClient.Delete(ctx, obj)
	}
	_ = k8sClient.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: resources.PVCName(inst), Namespace: namespace}})
	_ = k8sClient.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: resources.HonchoPVCName(inst), Namespace: namespace}})
	for _, obj := range waitObjects {
		key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
		Eventually(func() bool {
			next := obj.DeepCopyObject().(client.Object)
			err := k8sClient.Get(ctx, key, next)
			return apierrors.IsNotFound(err)
		}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())
	}
}
