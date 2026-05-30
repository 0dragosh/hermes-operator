package v1

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityValidationMarkers_MigrationExactlyOneSource(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("hermesinstance_types.go")
	require.NoError(t, err)

	assert.Contains(t, string(body), `+kubebuilder:validation:XValidation:rule="has(self.openclawInstanceRef) != has(self.backupRef)",message="set exactly one of openclawInstanceRef or backupRef"`)
}

func TestSecurityValidationMarkers_SelfConfigXORFields(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("hermesselfconfig_types.go")
	require.NoError(t, err)

	assert.Contains(t, string(body), `+kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="set exactly one of value or valueFrom"`)
	assert.Contains(t, string(body), `+kubebuilder:validation:XValidation:rule="has(self.secretKeyRef) != has(self.configMapKeyRef)",message="set exactly one of secretKeyRef or configMapKeyRef"`)
	assert.Contains(t, string(body), `+kubebuilder:validation:XValidation:rule="has(self.content) != has(self.contentFrom)",message="set exactly one of content or contentFrom"`)
}

func TestSecurityValidationMarkers_MetricsSecureDefault(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("hermesinstance_types.go")
	require.NoError(t, err)

	assert.Contains(t, string(body), "// +kubebuilder:default=true\n\t// +optional\n\tSecure *bool `json:\"secure,omitempty\"`")
}

func TestSecurityValidationMarkers_S3AndProfileSafety(t *testing.T) {
	t.Parallel()

	instanceBody, err := os.ReadFile("hermesinstance_types.go")
	require.NoError(t, err)
	selfConfigBody, err := os.ReadFile("hermesselfconfig_types.go")
	require.NoError(t, err)

	assert.Contains(t, string(instanceBody), "// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9.-]*[a-z0-9]$`\n\tBucket string `json:\"bucket\"`")
	assert.Contains(t, string(instanceBody), "// +kubebuilder:validation:Pattern=`^[^[:space:][:cntrl:]]+$`\n\tEndpoint string `json:\"endpoint\"`")
	assert.Contains(t, string(instanceBody), "// +kubebuilder:validation:Pattern=`^[^[:cntrl:]]+$`\n\tKey                  string               `json:\"key\"`")
	assert.Contains(t, string(selfConfigBody), "// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`\n\tProfileID string `json:\"profileID\"`")
}

func TestSecurityValidationMarkers_WorkspacePathSafety(t *testing.T) {
	t.Parallel()

	instanceBody, err := os.ReadFile("hermesinstance_types.go")
	require.NoError(t, err)
	selfConfigBody, err := os.ReadFile("hermesselfconfig_types.go")
	require.NoError(t, err)

	assert.Contains(t, string(instanceBody), "// +kubebuilder:validation:Pattern=`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`\n\tPath string `json:\"path\"`")
	assert.Contains(t, string(selfConfigBody), "// +kubebuilder:validation:Pattern=`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`\n\tPath string `json:\"path\"`")
	assert.NotContains(t, string(instanceBody), "self.contains('__')")
	assert.NotContains(t, string(selfConfigBody), "self.contains('__')")
}
