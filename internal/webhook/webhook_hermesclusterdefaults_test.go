package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func TestHCDValidator_AllowSingleton(t *testing.T) {
	t.Parallel()
	v := &HermesClusterDefaultsValidator{}
	hcd := &hermesv1.HermesClusterDefaults{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
	_, err := v.ValidateCreate(context.Background(), hcd)
	assert.NoError(t, err)
}

func TestHCDValidator_DenyOtherNames(t *testing.T) {
	t.Parallel()
	v := &HermesClusterDefaultsValidator{}
	for _, n := range []string{"default", "foo", "Cluster", "CLUSTER"} {
		hcd := &hermesv1.HermesClusterDefaults{ObjectMeta: metav1.ObjectMeta{Name: n}}
		_, err := v.ValidateCreate(context.Background(), hcd)
		assert.Errorf(t, err, "expected reject for name %q", n)
	}
}

func TestHCDValidator_DenyMutableOrIncompleteImageDefaults(t *testing.T) {
	t.Parallel()
	v := &HermesClusterDefaultsValidator{}

	for _, tc := range []struct {
		name  string
		image hermesv1.ImageSpec
		want  string
	}{
		{
			name:  "tag-based repository without tag",
			image: hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent"},
			want:  "spec.image.tag is required",
		},
		{
			name:  "latest tag",
			image: hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent", Tag: "latest"},
			want:  "must not be \"latest\"",
		},
		{
			name:  "malformed digest",
			image: hermesv1.ImageSpec{Repository: "ghcr.io/paperclipinc/hermes-agent@sha256:bad"},
			want:  "@sha256:<64 lowercase hex chars>",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hcd := &hermesv1.HermesClusterDefaults{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: hermesv1.HermesClusterDefaultsSpec{
					Image: tc.image,
				},
			}

			_, err := v.ValidateCreate(context.Background(), hcd)

			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

func TestHCDValidator_AllowPinnedImageDefaults(t *testing.T) {
	t.Parallel()
	v := &HermesClusterDefaultsValidator{}

	for _, image := range []hermesv1.ImageSpec{
		{Repository: "ghcr.io/paperclipinc/hermes-agent", Tag: hermesv1.DefaultAgentImageTag},
		{Repository: "ghcr.io/paperclipinc/hermes-agent@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{Tag: hermesv1.DefaultAgentImageTag},
	} {
		hcd := &hermesv1.HermesClusterDefaults{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: hermesv1.HermesClusterDefaultsSpec{
				Image: image,
			},
		}

		_, err := v.ValidateCreate(context.Background(), hcd)

		assert.NoError(t, err)
	}
}
