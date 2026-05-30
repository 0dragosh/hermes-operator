package controller

import (
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func TestMergeResults(t *testing.T) {
	tests := []struct {
		name    string
		current ctrl.Result
		next    ctrl.Result
		want    ctrl.Result
	}{
		{
			name:    "keeps existing requeueAfter when next is empty",
			current: ctrl.Result{RequeueAfter: 5 * time.Minute},
			next:    ctrl.Result{},
			want:    ctrl.Result{RequeueAfter: 5 * time.Minute},
		},
		{
			name:    "takes shorter positive requeueAfter",
			current: ctrl.Result{RequeueAfter: 5 * time.Minute},
			next:    ctrl.Result{RequeueAfter: 10 * time.Second},
			want:    ctrl.Result{RequeueAfter: 10 * time.Second},
		},
		{
			name:    "preserves explicit requeue",
			current: ctrl.Result{},
			next:    ctrl.Result{Requeue: true},
			want:    ctrl.Result{Requeue: true},
		},
		{
			name:    "keeps requeue and shorter delay together",
			current: ctrl.Result{Requeue: true, RequeueAfter: time.Minute},
			next:    ctrl.Result{RequeueAfter: 15 * time.Second},
			want:    ctrl.Result{Requeue: true, RequeueAfter: 15 * time.Second},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeResults(tt.current, tt.next)
			if got != tt.want {
				t.Fatalf("mergeResults() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRequiredConditionTypes(t *testing.T) {
	base := &hermesv1.HermesInstance{}
	wantBase := []string{
		hermesv1.ConditionTypeConfigReady,
		hermesv1.ConditionTypeSecretsReady,
		hermesv1.ConditionTypeServiceReady,
		conditionTypeStatefulSetReady,
		hermesv1.ConditionTypeStorageReady,
		hermesv1.ConditionTypeNetworkPolicyReady,
		hermesv1.ConditionTypeRBACReady,
	}
	if got := requiredConditionTypes(base); !reflect.DeepEqual(got, wantBase) {
		t.Fatalf("base requiredConditionTypes() = %#v, want %#v", got, wantBase)
	}

	disabled := &hermesv1.HermesInstance{
		Spec: hermesv1.HermesInstanceSpec{
			Storage: hermesv1.StorageSpec{
				Persistence: hermesv1.PersistenceSpec{Enabled: boolPtr(false)},
			},
			Security: hermesv1.SecuritySpec{
				NetworkPolicy: hermesv1.NetworkPolicySpec{Enabled: boolPtr(false)},
				RBAC:          hermesv1.RBACSpec{CreateServiceAccount: boolPtr(false)},
			},
		},
	}
	wantDisabled := []string{
		hermesv1.ConditionTypeConfigReady,
		hermesv1.ConditionTypeSecretsReady,
		hermesv1.ConditionTypeServiceReady,
		conditionTypeStatefulSetReady,
	}
	if got := requiredConditionTypes(disabled); !reflect.DeepEqual(got, wantDisabled) {
		t.Fatalf("disabled requiredConditionTypes() = %#v, want %#v", got, wantDisabled)
	}

	active := &hermesv1.HermesInstance{
		Spec: hermesv1.HermesInstanceSpec{
			Availability: hermesv1.AvailabilitySpec{
				PodDisruptionBudget: hermesv1.PDBSpec{Enabled: boolPtr(true)},
				HorizontalPodAutoscaler: hermesv1.HPASpec{
					Enabled: boolPtr(true),
				},
			},
			Networking: hermesv1.NetworkingSpec{
				Ingress: hermesv1.IngressSpec{Enabled: boolPtr(true)},
			},
			Observability: hermesv1.ObservabilitySpec{
				ServiceMonitor: hermesv1.ServiceMonitorSpec{Enabled: boolPtr(true)},
				PrometheusRule: hermesv1.PrometheusRuleSpec{Enabled: boolPtr(true)},
			},
			ProfileStore: hermesv1.ProfileStoreSpec{
				Honcho: hermesv1.HonchoSpec{Enabled: boolPtr(true)},
			},
			Backup:      hermesv1.BackupSpec{Schedule: "0 2 * * *"},
			RestoreFrom: "s3://bucket/snapshot.tar.zst",
			Migration: hermesv1.MigrationSpec{
				FromOpenClaw: &hermesv1.MigrationFromOpenClawSpec{Mode: "copy"},
			},
			AutoUpdate: hermesv1.AutoUpdateSpec{Enabled: true},
		},
	}
	wantActiveTail := []string{
		hermesv1.ConditionTypePDBReady,
		hermesv1.ConditionTypeHPAReady,
		hermesv1.ConditionTypeIngressReady,
		hermesv1.ConditionTypeServiceMonitorReady,
		hermesv1.ConditionTypePrometheusRuleReady,
		conditionTypeProfileStoreReady,
		hermesv1.ConditionBackupReady,
		hermesv1.ConditionRestoreApplied,
		hermesv1.ConditionMigrationCompleted,
		hermesv1.ConditionAutoUpdated,
	}
	gotActive := requiredConditionTypes(active)
	for _, want := range wantActiveTail {
		if !stringSliceContains(gotActive, want) {
			t.Fatalf("active requiredConditionTypes() missing %q: %#v", want, gotActive)
		}
	}
}

func TestIsInstanceReady(t *testing.T) {
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Generation: 2}}
	for _, conditionType := range requiredConditionTypes(inst) {
		setTrueTestCondition(inst, conditionType)
	}
	if !isInstanceReady(inst) {
		t.Fatalf("isInstanceReady() = false, want true")
	}

	setFalseTestCondition(inst, hermesv1.ConditionTypeStorageReady)
	if isInstanceReady(inst) {
		t.Fatalf("isInstanceReady() = true with StorageReady=False, want false")
	}
	wantFailing := []string{hermesv1.ConditionTypeStorageReady}
	if got := failingRequiredConditions(inst); !reflect.DeepEqual(got, wantFailing) {
		t.Fatalf("failingRequiredConditions() = %#v, want %#v", got, wantFailing)
	}
}

func TestIsInstanceReadyRejectsStaleConditionGeneration(t *testing.T) {
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Generation: 2}}
	for _, conditionType := range requiredConditionTypes(inst) {
		setTrueTestCondition(inst, conditionType)
	}
	for i := range inst.Status.Conditions {
		if inst.Status.Conditions[i].Type == hermesv1.ConditionTypeStorageReady {
			inst.Status.Conditions[i].ObservedGeneration = 1
		}
	}

	if isInstanceReady(inst) {
		t.Fatalf("isInstanceReady() = true with stale StorageReady observedGeneration, want false")
	}
	wantFailing := []string{hermesv1.ConditionTypeStorageReady}
	if got := failingRequiredConditions(inst); !reflect.DeepEqual(got, wantFailing) {
		t.Fatalf("failingRequiredConditions() = %#v, want %#v", got, wantFailing)
	}
}

func setTrueTestCondition(inst *hermesv1.HermesInstance, conditionType string) {
	setTestCondition(inst, conditionType, metav1.ConditionTrue)
}

func setFalseTestCondition(inst *hermesv1.HermesInstance, conditionType string) {
	setTestCondition(inst, conditionType, metav1.ConditionFalse)
}

func setTestCondition(inst *hermesv1.HermesInstance, conditionType string, status metav1.ConditionStatus) {
	meta.SetStatusCondition(&inst.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             "Test",
		Message:            "test",
		ObservedGeneration: inst.Generation,
	})
}

func boolPtr(v bool) *bool {
	return &v
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
