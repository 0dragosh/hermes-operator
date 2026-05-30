package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func TestBuildNetworkPolicy_DenyAllBase(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"}}
	np := BuildNetworkPolicy(inst)
	assert.Equal(t, "demo", np.Name)
	assert.Equal(t, "agents", np.Namespace)
	assert.Contains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.Contains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
	assert.Equal(t, "demo", np.Spec.PodSelector.MatchLabels["app.kubernetes.io/instance"])
	assert.Equal(t, "hermes-agent", np.Spec.PodSelector.MatchLabels["app.kubernetes.io/name"])
}

func TestBuildNetworkPolicy_SameNamespaceIngress(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"}}
	np := BuildNetworkPolicy(inst)
	assert.False(t, hasIngressFromNamespace(np, "agents"), "same-namespace ingress should be opt-in")
}

func TestBuildNetworkPolicy_AllowSameNamespaceIngress(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Security: hermesv1.SecuritySpec{
				NetworkPolicy: hermesv1.NetworkPolicySpec{AllowSameNamespaceIngress: Ptr(true)},
			},
		},
	}
	np := BuildNetworkPolicy(inst)
	assert.True(t, hasIngressFromNamespace(np, "agents"), "expected opt-in ingress rule from same namespace")
}

func TestBuildNetworkPolicy_DefaultDNSEgress(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: "demo"}}
	np := BuildNetworkPolicy(inst)
	foundUDP53, foundTCP53 := false, false
	for _, rule := range np.Spec.Egress {
		for _, p := range rule.Ports {
			if p.Protocol != nil && *p.Protocol == corev1.ProtocolUDP && p.Port != nil && p.Port.IntValue() == 53 {
				foundUDP53 = true
			}
			if p.Protocol != nil && *p.Protocol == corev1.ProtocolTCP && p.Port != nil && p.Port.IntValue() == 53 {
				foundTCP53 = true
			}
		}
	}
	assert.True(t, foundUDP53, "default-allow DNS UDP/53")
	assert.True(t, foundTCP53, "default-allow DNS TCP/53")
	assert.False(t, hasEmptyPeerTCPPort(np.Spec.Egress, 443), "default egress should not allow TCP/443 to all destinations")
}

func TestBuildNetworkPolicy_AllowDNSDisabled(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Security: hermesv1.SecuritySpec{
				NetworkPolicy: hermesv1.NetworkPolicySpec{AllowDNS: Ptr(false)},
			},
		},
	}
	np := BuildNetworkPolicy(inst)
	for _, rule := range np.Spec.Egress {
		for _, p := range rule.Ports {
			if p.Port != nil && p.Port.IntValue() == 53 {
				t.Fatalf("expected no DNS rule when AllowDNS=false")
			}
		}
	}
}

func TestBuildNetworkPolicy_AllowedIngressNamespacesAndCIDRs(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Security: hermesv1.SecuritySpec{
				NetworkPolicy: hermesv1.NetworkPolicySpec{
					AllowedIngressNamespaces: []string{"prometheus"},
					AllowedIngressCIDRs:      []string{"10.0.0.0/8"},
				},
			},
		},
	}
	np := BuildNetworkPolicy(inst)
	var sawCIDR bool
	for _, rule := range np.Spec.Ingress {
		for _, from := range rule.From {
			if from.IPBlock != nil && from.IPBlock.CIDR == "10.0.0.0/8" {
				sawCIDR = true
			}
		}
	}
	assert.True(t, hasIngressFromNamespace(np, "prometheus"), "expected ingress rule for namespace prometheus")
	assert.True(t, sawCIDR, "expected ingress rule for CIDR 10.0.0.0/8")
}

func TestBuildNetworkPolicy_AdditionalEgress(t *testing.T) {
	t.Parallel()
	extra := networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "203.0.113.0/24"}}},
	}
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: hermesv1.HermesInstanceSpec{
			Security: hermesv1.SecuritySpec{
				NetworkPolicy: hermesv1.NetworkPolicySpec{AdditionalEgress: []networkingv1.NetworkPolicyEgressRule{extra}},
			},
		},
	}
	np := BuildNetworkPolicy(inst)
	var sawExtra bool
	for _, rule := range np.Spec.Egress {
		for _, peer := range rule.To {
			if peer.IPBlock != nil && peer.IPBlock.CIDR == "203.0.113.0/24" {
				sawExtra = true
			}
		}
	}
	assert.True(t, sawExtra)
}

func TestNetworkPolicyName(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: "demo"}}
	assert.Equal(t, "demo", NetworkPolicyName(inst))
	_ = metav1.ObjectMeta{}
}

func TestExtraEgressRules_TelegramAndDiscord(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			Gateways: hermesv1.GatewaysSpec{
				Telegram: hermesv1.TelegramGatewaySpec{Enabled: Ptr(true)},
				Discord:  hermesv1.DiscordGatewaySpec{Enabled: Ptr(true)},
			},
		},
	}
	rules := ExtraEgressRules(inst)
	assert.False(t, hasEmptyPeerTCPPort(rules, 443), "gateway enablement should not open TCP/443 to all destinations")
}

func TestExtraEgressRules_HonchoSibling(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			ProfileStore: hermesv1.ProfileStoreSpec{
				Honcho: hermesv1.HonchoSpec{Enabled: Ptr(true)},
			},
		},
	}
	rules := ExtraEgressRules(inst)
	foundHoncho := false
	for _, r := range rules {
		for _, peer := range r.To {
			if peer.PodSelector != nil && peer.PodSelector.MatchLabels["app.kubernetes.io/instance"] == "demo-honcho" {
				foundHoncho = true
			}
		}
	}
	assert.True(t, foundHoncho, "egress to honcho sibling pod selector present")
}

func TestBuildHonchoNetworkPolicy_IngressOnlyFromHermes(t *testing.T) {
	t.Parallel()
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
		Spec: hermesv1.HermesInstanceSpec{
			ProfileStore: hermesv1.ProfileStoreSpec{
				Honcho: hermesv1.HonchoSpec{Enabled: Ptr(true)},
			},
		},
	}
	np := BuildHonchoNetworkPolicy(inst)
	assert.Equal(t, "demo-honcho", np.Name)
	assert.Equal(t, "honcho", np.Spec.PodSelector.MatchLabels["app.kubernetes.io/name"])

	require := np.Spec.Ingress
	assert.Len(t, require, 1)
	from := require[0].From
	assert.Len(t, from, 1)
	assert.Equal(t, "hermes-agent", from[0].PodSelector.MatchLabels["app.kubernetes.io/name"])
	assert.Equal(t, "demo", from[0].PodSelector.MatchLabels["app.kubernetes.io/instance"])

	assert.Empty(t, np.Spec.Egress)
	assert.Contains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
}

func hasIngressFromNamespace(np *networkingv1.NetworkPolicy, namespace string) bool {
	for _, rule := range np.Spec.Ingress {
		for _, from := range rule.From {
			if from.NamespaceSelector != nil &&
				from.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == namespace {
				return true
			}
		}
	}
	return false
}

func hasEmptyPeerTCPPort(rules []networkingv1.NetworkPolicyEgressRule, port int) bool {
	for _, rule := range rules {
		if len(rule.To) != 0 {
			continue
		}
		for _, p := range rule.Ports {
			if p.Protocol != nil && *p.Protocol != corev1.ProtocolTCP {
				continue
			}
			if p.Port != nil && p.Port.IntValue() == port {
				return true
			}
		}
	}
	return false
}
