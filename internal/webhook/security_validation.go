package webhook

import (
	"fmt"
	pathpkg "path"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

type unsafeAnnotationRule struct {
	key    string
	values []string
	reason string
}

var unsafeServiceAnnotationRules = []unsafeAnnotationRule{
	{key: "service.beta.kubernetes.io/aws-load-balancer-scheme", values: []string{"internet-facing"}, reason: "public load balancers are not allowed"},
	{key: "service.beta.kubernetes.io/aws-load-balancer-internal", values: []string{"false"}, reason: "public load balancers are not allowed"},
	{key: "cloud.google.com/load-balancer-type", values: []string{"external"}, reason: "public load balancers are not allowed"},
	{key: "networking.gke.io/load-balancer-type", values: []string{"external"}, reason: "public load balancers are not allowed"},
	{key: "service.beta.kubernetes.io/azure-load-balancer-internal", values: []string{"false"}, reason: "public load balancers are not allowed"},
}

var unsafeIngressAnnotationRules = []unsafeAnnotationRule{
	{key: "alb.ingress.kubernetes.io/scheme", values: []string{"internet-facing"}, reason: "public load balancers are not allowed"},
	{key: "nginx.ingress.kubernetes.io/ssl-redirect", values: []string{"false"}, reason: "TLS redirects must stay enabled"},
	{key: "nginx.ingress.kubernetes.io/force-ssl-redirect", values: []string{"false"}, reason: "TLS redirects must stay enabled"},
	{key: "ingress.kubernetes.io/ssl-redirect", values: []string{"false"}, reason: "TLS redirects must stay enabled"},
	{key: "traefik.ingress.kubernetes.io/router.tls", values: []string{"false"}, reason: "TLS must stay enabled"},
	{key: "haproxy.org/ssl-redirect", values: []string{"false"}, reason: "TLS redirects must stay enabled"},
	{key: "kubernetes.io/ingress.allow-http", values: []string{"true"}, reason: "plain HTTP ingress is not allowed"},
	{key: "nginx.ingress.kubernetes.io/enable-global-auth", values: []string{"false"}, reason: "auth must stay enabled"},
	{key: "nginx.ingress.kubernetes.io/auth-type", values: []string{"none", "off", "false", "disabled"}, reason: "auth must stay enabled"},
}

func validateAdmissionGuardrails(inst *hermesv1.HermesInstance) field.ErrorList {
	errs := field.ErrorList{}
	errs = append(errs, validateUnsupportedRuntime(inst)...)
	errs = append(errs, validateWorkloadSecurity(inst)...)
	errs = append(errs, validateNetworkExposure(inst)...)
	return errs
}

func validateUnsupportedRuntime(inst *hermesv1.HermesInstance) field.ErrorList {
	if len(inst.Spec.Runtime.ExtraAptPackages) == 0 {
		return nil
	}
	return field.ErrorList{field.Forbidden(
		field.NewPath("spec", "runtime", "extraAptPackages"),
		"runtime.extraAptPackages is unsupported because init-container package installs do not affect the main container filesystem; build a custom agent image with the required OS packages",
	)}
}

func validateWorkloadSecurity(inst *hermesv1.HermesInstance) field.ErrorList {
	errs := field.ErrorList{}
	specPath := field.NewPath("spec")
	errs = append(errs, validateFutureHostNamespaceFields(specPath, reflect.ValueOf(inst.Spec))...)
	errs = append(errs, validatePodSecurityContext(specPath.Child("security", "podSecurityContext"), inst.Spec.Security.PodSecurityContext)...)
	errs = append(errs, validateContainerSecurityContext(specPath.Child("security", "containerSecurityContext"), inst.Spec.Security.ContainerSecurityContext)...)
	errs = append(errs, validateVolumeMounts(specPath.Child("extraVolumeMounts"), inst.Spec.ExtraVolumeMounts)...)
	errs = append(errs, validateExtraVolumes(specPath.Child("extraVolumes"), inst.Spec.ExtraVolumes)...)

	for i := range inst.Spec.InitContainers {
		container := inst.Spec.InitContainers[i]
		containerPath := specPath.Child("initContainers").Index(i)
		errs = append(errs, validateContainerSecurityContext(containerPath.Child("securityContext"), container.SecurityContext)...)
		errs = append(errs, validateContainerPorts(containerPath.Child("ports"), container.Ports)...)
		errs = append(errs, validateVolumeMounts(containerPath.Child("volumeMounts"), container.VolumeMounts)...)
	}
	for i := range inst.Spec.Sidecars {
		container := inst.Spec.Sidecars[i]
		containerPath := specPath.Child("sidecars").Index(i)
		errs = append(errs, validateContainerSecurityContext(containerPath.Child("securityContext"), container.SecurityContext)...)
		errs = append(errs, validateContainerPorts(containerPath.Child("ports"), container.Ports)...)
		errs = append(errs, validateVolumeMounts(containerPath.Child("volumeMounts"), container.VolumeMounts)...)
	}
	return errs
}

func validatePodSecurityContext(path *field.Path, ctx *corev1.PodSecurityContext) field.ErrorList {
	if ctx == nil {
		return nil
	}
	errs := field.ErrorList{}
	if ctx.RunAsNonRoot != nil && !*ctx.RunAsNonRoot {
		errs = append(errs, field.Forbidden(path.Child("runAsNonRoot"), "runAsNonRoot=false is not allowed"))
	}
	if ctx.RunAsUser != nil && *ctx.RunAsUser == 0 {
		errs = append(errs, field.Forbidden(path.Child("runAsUser"), "runAsUser=0 is not allowed"))
	}
	if ctx.SeccompProfile != nil && ctx.SeccompProfile.Type == corev1.SeccompProfileTypeUnconfined {
		errs = append(errs, field.Forbidden(path.Child("seccompProfile"), "seccompProfile.type=Unconfined is not allowed"))
	}
	return errs
}

func validateContainerSecurityContext(path *field.Path, ctx *corev1.SecurityContext) field.ErrorList {
	if ctx == nil {
		return nil
	}
	errs := field.ErrorList{}
	if ctx.Privileged != nil && *ctx.Privileged {
		errs = append(errs, field.Forbidden(path.Child("privileged"), "securityContext.privileged=true is not allowed"))
	}
	if ctx.AllowPrivilegeEscalation != nil && *ctx.AllowPrivilegeEscalation {
		errs = append(errs, field.Forbidden(path.Child("allowPrivilegeEscalation"), "allowPrivilegeEscalation=true is not allowed"))
	}
	if ctx.RunAsNonRoot != nil && !*ctx.RunAsNonRoot {
		errs = append(errs, field.Forbidden(path.Child("runAsNonRoot"), "runAsNonRoot=false is not allowed"))
	}
	if ctx.RunAsUser != nil && *ctx.RunAsUser == 0 {
		errs = append(errs, field.Forbidden(path.Child("runAsUser"), "runAsUser=0 is not allowed"))
	}
	if ctx.ReadOnlyRootFilesystem != nil && !*ctx.ReadOnlyRootFilesystem {
		errs = append(errs, field.Forbidden(path.Child("readOnlyRootFilesystem"), "readOnlyRootFilesystem=false is not allowed"))
	}
	if ctx.Capabilities != nil && len(ctx.Capabilities.Add) > 0 {
		errs = append(errs, field.Forbidden(path.Child("capabilities", "add"), "adding Linux capabilities is not allowed"))
	}
	if ctx.Capabilities != nil && len(ctx.Capabilities.Drop) > 0 && !capabilitiesDropAll(ctx.Capabilities.Drop) {
		errs = append(errs, field.Forbidden(path.Child("capabilities", "drop"), "capabilities.drop must include ALL"))
	}
	if ctx.SeccompProfile != nil && ctx.SeccompProfile.Type == corev1.SeccompProfileTypeUnconfined {
		errs = append(errs, field.Forbidden(path.Child("seccompProfile"), "seccompProfile.type=Unconfined is not allowed"))
	}
	return errs
}

func capabilitiesDropAll(drop []corev1.Capability) bool {
	for _, cap := range drop {
		if cap == "ALL" {
			return true
		}
	}
	return false
}

func validateContainerPorts(path *field.Path, ports []corev1.ContainerPort) field.ErrorList {
	errs := field.ErrorList{}
	for i := range ports {
		if ports[i].HostPort != 0 {
			errs = append(errs, field.Forbidden(path.Index(i).Child("hostPort"), "hostPort is not allowed"))
		}
		if ports[i].HostIP != "" {
			errs = append(errs, field.Forbidden(path.Index(i).Child("hostIP"), "hostIP is not allowed"))
		}
	}
	return errs
}

func validateExtraVolumes(path *field.Path, volumes []corev1.Volume) field.ErrorList {
	errs := field.ErrorList{}
	for i := range volumes {
		volume := volumes[i]
		volumePath := path.Index(i)
		if volume.HostPath != nil {
			errs = append(errs, field.Forbidden(volumePath.Child("hostPath"), "hostPath volumes are not allowed"))
		}
		if volume.Projected != nil {
			for j := range volume.Projected.Sources {
				if volume.Projected.Sources[j].ServiceAccountToken != nil {
					errs = append(errs, field.Forbidden(
						volumePath.Child("projected", "sources").Index(j).Child("serviceAccountToken"),
						"projected serviceAccountToken volumes are not allowed",
					))
				}
			}
		}
	}
	return errs
}

func validateVolumeMounts(path *field.Path, mounts []corev1.VolumeMount) field.ErrorList {
	errs := field.ErrorList{}
	for i := range mounts {
		mountPath := mounts[i].MountPath
		if isUnsafeMountPath(mountPath) {
			errs = append(errs, field.Forbidden(
				path.Index(i).Child("mountPath"),
				fmt.Sprintf("mounting %q is not allowed", mountPath),
			))
		}
	}
	return errs
}

func isUnsafeMountPath(mountPath string) bool {
	if mountPath == "" {
		return false
	}
	cleaned := pathpkg.Clean(mountPath)
	return cleaned == "/" ||
		cleaned == "/proc" ||
		strings.HasPrefix(cleaned, "/proc/") ||
		cleaned == "/sys" ||
		strings.HasPrefix(cleaned, "/sys/") ||
		cleaned == "/var/run/docker.sock"
}

func validateFutureHostNamespaceFields(path *field.Path, spec reflect.Value) field.ErrorList {
	if spec.Kind() == reflect.Pointer {
		if spec.IsNil() {
			return nil
		}
		spec = spec.Elem()
	}
	if spec.Kind() != reflect.Struct {
		return nil
	}

	errs := field.ErrorList{}
	for _, tc := range []struct {
		goName   string
		jsonName string
	}{
		{goName: "HostNetwork", jsonName: "hostNetwork"},
		{goName: "HostPID", jsonName: "hostPID"},
		{goName: "HostIPC", jsonName: "hostIPC"},
	} {
		if fieldValueTrue(spec.FieldByName(tc.goName)) {
			errs = append(errs, field.Forbidden(path.Child(tc.jsonName), fmt.Sprintf("%s is not allowed", tc.jsonName)))
		}
	}
	return errs
}

func fieldValueTrue(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	return value.Kind() == reflect.Bool && value.Bool()
}

func validateNetworkExposure(inst *hermesv1.HermesInstance) field.ErrorList {
	errs := field.ErrorList{}
	path := field.NewPath("spec", "networking")
	service := inst.Spec.Networking.Service
	switch service.Type {
	case corev1.ServiceTypeNodePort:
		errs = append(errs, field.Forbidden(path.Child("service", "type"), "service type NodePort is not allowed"))
	case corev1.ServiceTypeLoadBalancer:
		errs = append(errs, field.Forbidden(path.Child("service", "type"), "service type LoadBalancer is not allowed"))
	}
	errs = append(errs, validateUnsafeAnnotations(path.Child("service", "annotations"), service.Annotations, unsafeServiceAnnotationRules)...)

	ingress := inst.Spec.Networking.Ingress
	ingressPath := path.Child("ingress")
	if ingress.Enabled != nil && *ingress.Enabled && len(ingress.TLS) == 0 {
		errs = append(errs, field.Required(ingressPath.Child("tls"), "TLS is required when ingress is enabled"))
	}
	errs = append(errs, validateUnsafeAnnotations(ingressPath.Child("annotations"), ingress.Annotations, unsafeIngressAnnotationRules)...)

	metrics := inst.Spec.Observability.Metrics
	if metrics.Secure != nil && !*metrics.Secure && (metrics.Enabled == nil || *metrics.Enabled) {
		errs = append(errs, field.Forbidden(
			field.NewPath("spec", "observability", "metrics", "secure"),
			"metrics enabled requires secure=true",
		))
	}
	return errs
}

func validateUnsafeAnnotations(path *field.Path, annotations map[string]string, rules []unsafeAnnotationRule) field.ErrorList {
	errs := field.ErrorList{}
	for key, value := range annotations {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		normalizedValue := strings.ToLower(strings.TrimSpace(value))
		for _, rule := range rules {
			if normalizedKey != rule.key {
				continue
			}
			if stringInSlice(normalizedValue, rule.values) {
				errs = append(errs, field.Forbidden(
					path.Key(key),
					fmt.Sprintf("%s=%q is not allowed: %s", key, value, rule.reason),
				))
			}
		}
	}
	return errs
}

func stringInSlice(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
