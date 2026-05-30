package webhook

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

// HermesClusterDefaultsValidator enforces design §6: name must be "cluster".
type HermesClusterDefaultsValidator struct{}

var _ admission.CustomValidator = &HermesClusterDefaultsValidator{}

func (v *HermesClusterDefaultsValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	return validateHCD(obj)
}

func (v *HermesClusterDefaultsValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	return validateHCD(newObj)
}

func (v *HermesClusterDefaultsValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateHCD(obj runtime.Object) (admission.Warnings, error) {
	hcd, ok := obj.(*hermesv1.HermesClusterDefaults)
	if !ok {
		return nil, fmt.Errorf("expected *HermesClusterDefaults, got %T", obj)
	}
	if hcd.Name != "cluster" {
		return nil, fmt.Errorf("HermesClusterDefaults must be the singleton named \"cluster\" (got %q)", hcd.Name)
	}
	if err := validateHCDImageDefaults(hcd.Spec.Image); err != nil {
		return nil, err
	}
	return nil, nil
}

func validateHCDImageDefaults(image hermesv1.ImageSpec) error {
	if strings.Contains(image.Repository, "@sha256:") && !hermesv1.ImageRepositoryUsesDigest(image.Repository) {
		return fmt.Errorf("spec.image.repository digest references must use @sha256:<64 lowercase hex chars>")
	}
	if image.Tag == "latest" {
		return fmt.Errorf("spec.image.tag must not be \"latest\" for HermesClusterDefaults image defaults")
	}
	if image.Repository != "" && !hermesv1.ImageRepositoryUsesDigest(image.Repository) && image.Tag == "" {
		return fmt.Errorf("spec.image.tag is required when HermesClusterDefaults sets a tag-based image repository")
	}
	return nil
}
