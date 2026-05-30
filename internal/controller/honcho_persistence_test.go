package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

var _ = Describe("HermesInstance reconciler: Honcho persistence", func() {
	const (
		instName = "honcho-no-pvc"
		ns       = "default"
	)

	AfterEach(func() {
		ctx := context.Background()
		_ = k8sClient.Delete(ctx, &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: instName, Namespace: ns}})
		cleanupHermesInstanceOwnedResources(ctx, instName, ns)
	})

	It("skips the Honcho PVC and mounts emptyDir when persistence is disabled", func() {
		ctx := context.Background()
		inst := &hermesv1.HermesInstance{
			ObjectMeta: metav1.ObjectMeta{Name: instName, Namespace: ns},
			Spec: hermesv1.HermesInstanceSpec{
				Image: hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent"},
				ProfileStore: hermesv1.ProfileStoreSpec{
					Honcho: hermesv1.HonchoSpec{
						Enabled: Ptr(true),
						Persistence: hermesv1.HonchoPersistenceSpec{
							Enabled: Ptr(false),
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, inst)).To(Succeed())

		Eventually(func(g Gomega) {
			var dep appsv1.Deployment
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: instName + "-honcho", Namespace: ns}, &dep)).To(Succeed())
			vol := honchoDataVolume(dep.Spec.Template.Spec.Volumes)
			g.Expect(vol).NotTo(BeNil())
			g.Expect(vol.EmptyDir).NotTo(BeNil())
			g.Expect(vol.PersistentVolumeClaim).To(BeNil())
		}, 30*time.Second, 250*time.Millisecond).Should(Succeed())

		Consistently(func() bool {
			var pvc corev1.PersistentVolumeClaim
			err := k8sClient.Get(ctx, types.NamespacedName{Name: instName + "-honcho-data", Namespace: ns}, &pvc)
			return apierrors.IsNotFound(err)
		}, time.Second, 100*time.Millisecond).Should(BeTrue())
	})
})

func honchoDataVolume(volumes []corev1.Volume) *corev1.Volume {
	for i, volume := range volumes {
		if volume.Name == "honcho-data" {
			return &volumes[i]
		}
	}
	return nil
}
