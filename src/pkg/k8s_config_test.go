package pkg

import (
	"testing"

	"github.com/rocktavious/autopilot/v2023"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
)

func TestReadPodConfig_EphemeralStorageUnsetByDefault(t *testing.T) {
	// Arrange
	viper.Reset()
	t.Cleanup(viper.Reset)
	// Act
	config, err := ReadPodConfig("does-not-exist.yaml")
	// Assert
	autopilot.Ok(t, err)
	_, hasRequest := config.Resources.Requests[corev1.ResourceEphemeralStorage]
	_, hasLimit := config.Resources.Limits[corev1.ResourceEphemeralStorage]
	autopilot.Equals(t, false, hasRequest)
	autopilot.Equals(t, false, hasLimit)
}

func TestReadPodConfig_EphemeralStorageSet(t *testing.T) {
	// Arrange
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("job-pod-requests-ephemeral-storage", 5120)
	viper.Set("job-pod-limits-ephemeral-storage", 20480)
	// Act
	config, err := ReadPodConfig("does-not-exist.yaml")
	// Assert
	autopilot.Ok(t, err)
	request := config.Resources.Requests[corev1.ResourceEphemeralStorage]
	limit := config.Resources.Limits[corev1.ResourceEphemeralStorage]
	autopilot.Equals(t, "5Gi", request.String())
	autopilot.Equals(t, "20Gi", limit.String())
}
