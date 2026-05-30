/*
Copyright 2026 Paperclip.inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package resources

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
)

func TestBuildSnapshotJob_NameAndMounts(t *testing.T) {
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
	}
	stamp := time.Date(2026, 5, 12, 8, 0, 0, 0, time.UTC)
	job := BuildSnapshotJob(inst, "user-42", "snapshot-payload", stamp)
	assert.Equal(t, "demo-snapshot-user-42-20260512080000", job.Name)
	assert.Equal(t, "agents", job.Namespace)

	spec := job.Spec.Template.Spec
	assert.Equal(t, corev1.RestartPolicyNever, spec.RestartPolicy, "Jobs use RestartPolicyNever")
	assert.Len(t, spec.Containers, 1)

	mounts := spec.Containers[0].VolumeMounts
	assert.Len(t, mounts, 1)
	assert.Equal(t, "honcho-data", mounts[0].Name)
	assert.Equal(t, "/data", mounts[0].MountPath)

	vols := spec.Volumes
	assert.Len(t, vols, 1)
	assert.NotNil(t, vols[0].PersistentVolumeClaim)
	assert.Equal(t, "demo-honcho-data", vols[0].PersistentVolumeClaim.ClaimName)
}

func TestBuildSnapshotJob_HardenedSecurity(t *testing.T) {
	inst := &hermesv1.HermesInstance{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "y"}}
	job := BuildSnapshotJob(inst, "p", "data", time.Now())
	spec := job.Spec.Template.Spec
	require.NotNil(t, spec.AutomountServiceAccountToken)
	assert.False(t, *spec.AutomountServiceAccountToken)

	c := job.Spec.Template.Spec.Containers[0]
	assert.NotNil(t, c.SecurityContext)
	assert.True(t, *c.SecurityContext.ReadOnlyRootFilesystem)
	assert.False(t, *c.SecurityContext.AllowPrivilegeEscalation)
	assert.Equal(t, []corev1.Capability{"ALL"}, c.SecurityContext.Capabilities.Drop)
}

func TestBuildSnapshotJob_StaticCommandAndEnv(t *testing.T) {
	inst := &hermesv1.HermesInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "agents"},
	}
	when := time.Date(2026, 5, 12, 8, 0, 0, 0, time.UTC)
	profileID := "prod-user-1"
	data := `{"payload":"prod; touch /tmp/pwned"}`

	job := BuildSnapshotJob(inst, profileID, data, when)
	c := job.Spec.Template.Spec.Containers[0]

	commandText := strings.Join(append(c.Command, c.Args...), "\n")
	assert.NotContains(t, commandText, profileID)
	assert.NotContains(t, commandText, data)
	assert.Equal(t, []string{"/bin/sh", "-c"}, c.Command)
	require.Len(t, c.Args, 1)
	assert.Contains(t, c.Args[0], `printf '%s' "$SNAPSHOT_DATA" > "$SNAPSHOT_PATH"`)

	env := map[string]string{}
	for _, item := range c.Env {
		env[item.Name] = item.Value
	}
	assert.Equal(t, "/data/snapshots/prod-user-1/2026-05-12T08:00:00Z.json", env["SNAPSHOT_PATH"])
	assert.Equal(t, data, env["SNAPSHOT_DATA"])
}
