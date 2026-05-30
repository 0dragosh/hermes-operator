/*
Copyright 2026 Paperclip.inc. Apache-2.0.
*/

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

// HermesSelfConfigValidator validates HermesSelfConfig creates and updates.
// It checks the existence of the parent HermesInstance, the well-formedness
// of patchConfig (must be valid JSON), and a few cross-field invariants
// (e.g. addProfileSnapshot requires honcho enabled on the parent).
//
// +kubebuilder:webhook:path=/validate-hermes-agent-v1-hermesselfconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=hermes.agent,resources=hermesselfconfigs,verbs=create;update,versions=v1,name=vhermesselfconfig.kb.io,admissionReviewVersions=v1
type HermesSelfConfigValidator struct {
	Client client.Client
}

var _ admission.CustomValidator = (*HermesSelfConfigValidator)(nil)

var selfConfigProfileIDPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func (v *HermesSelfConfigValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

func (v *HermesSelfConfigValidator) ValidateUpdate(ctx context.Context, _, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

func (v *HermesSelfConfigValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *HermesSelfConfigValidator) validate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	sc, ok := obj.(*hermesv1.HermesSelfConfig)
	if !ok {
		return nil, fmt.Errorf("expected HermesSelfConfig, got %T", obj)
	}

	if sc.Spec.InstanceRef == "" {
		return nil, fmt.Errorf("spec.instanceRef is required")
	}

	if errs := validateSelfConfigValueSources(sc); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}

	if sc.Spec.AddProfileSnapshot != nil {
		profileID := sc.Spec.AddProfileSnapshot.ProfileID
		if len(profileID) > 253 || !selfConfigProfileIDPattern.MatchString(profileID) {
			return nil, fmt.Errorf("spec.addProfileSnapshot.profileID %q must match ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ and be at most 253 characters", profileID)
		}
	}

	if v.Client != nil {
		parent := &hermesv1.HermesInstance{}
		err := v.Client.Get(ctx, types.NamespacedName{Name: sc.Spec.InstanceRef, Namespace: sc.Namespace}, parent)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("spec.instanceRef %q: no HermesInstance with that name in namespace %q", sc.Spec.InstanceRef, sc.Namespace)
			}
			return nil, fmt.Errorf("loading parent instance: %w", err)
		}
		if sc.Spec.AddProfileSnapshot != nil {
			if parent.Spec.ProfileStore.Honcho.Enabled == nil || !*parent.Spec.ProfileStore.Honcho.Enabled {
				return nil, fmt.Errorf("spec.addProfileSnapshot requires parent .spec.profileStore.honcho.enabled=true")
			}
		}
	}

	if sc.Spec.PatchConfig != nil && len(sc.Spec.PatchConfig.Raw) > 0 {
		var tmp map[string]interface{}
		if err := json.Unmarshal(sc.Spec.PatchConfig.Raw, &tmp); err != nil {
			return nil, fmt.Errorf("spec.patchConfig is not a valid JSON merge patch: %w", err)
		}
	}

	mutations := 0
	for _, has := range []bool{
		len(sc.Spec.AddSkills) > 0,
		sc.Spec.PatchConfig != nil && len(sc.Spec.PatchConfig.Raw) > 0,
		len(sc.Spec.AddEnvVars) > 0,
		len(sc.Spec.AddWorkspaceFiles) > 0,
		sc.Spec.AddProfileSnapshot != nil,
	} {
		if has {
			mutations++
		}
	}
	if mutations > 1 {
		return admission.Warnings{
			"this HermesSelfConfig requests multiple mutations; consider one mutation per resource for atomic audit trails",
		}, nil
	}
	return nil, nil
}

func validateSelfConfigValueSources(sc *hermesv1.HermesSelfConfig) field.ErrorList {
	errs := field.ErrorList{}
	envPath := field.NewPath("spec", "addEnvVars")
	for i, env := range sc.Spec.AddEnvVars {
		itemPath := envPath.Index(i)
		hasValue := env.Value != nil
		hasValueFrom := env.ValueFrom != nil
		if hasValue == hasValueFrom {
			errs = append(errs, field.Invalid(
				itemPath,
				env.Name,
				"set exactly one of value or valueFrom",
			))
		}
		if env.ValueFrom != nil {
			hasSecret := env.ValueFrom.SecretKeyRef != nil
			hasConfigMap := env.ValueFrom.ConfigMapKeyRef != nil
			if hasSecret == hasConfigMap {
				errs = append(errs, field.Invalid(
					itemPath.Child("valueFrom"),
					env.Name,
					"set exactly one of secretKeyRef or configMapKeyRef",
				))
			}
		}
	}

	filePath := field.NewPath("spec", "addWorkspaceFiles")
	for i, file := range sc.Spec.AddWorkspaceFiles {
		itemPath := filePath.Index(i)
		errs = append(errs, validateWorkspaceRelativePath(itemPath.Child("path"), file.Path, 512)...)
		hasContent := file.Content != nil
		hasContentFrom := file.ContentFrom != nil
		if hasContent == hasContentFrom {
			errs = append(errs, field.Invalid(
				itemPath,
				file.Path,
				"set exactly one of content or contentFrom",
			))
		}
	}
	return errs
}
