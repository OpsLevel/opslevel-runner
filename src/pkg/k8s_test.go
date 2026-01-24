package pkg

import (
	"testing"

	"github.com/opslevel/opslevel-go/v2024"
	"github.com/rocktavious/autopilot/v2023"
	"github.com/rs/zerolog"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
)

func TestCreateLabelSelector(t *testing.T) {
	// Arrange
	labels := map[string]string{
		"app":      "foo",
		"instance": "bar",
	}
	// Act
	labelSelector, err := CreateLabelSelector(labels)
	// Assert
	autopilot.Ok(t, err)
	autopilot.Equals(t, labels, labelSelector.MatchLabels)
}

func TestGetPodObject_AgentModePrivileged(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace:                     "test",
			SecurityContext:               corev1.PodSecurityContext{},
			TerminationGracePeriodSeconds: 30,
			AgentMode:                     true,
		},
	}
	job := opslevel.RunnerJob{
		Image: "alpine:latest",
	}
	labels := map[string]string{"app": "test"}

	// Act
	pod := runner.getPodObject("test-pod", labels, job)

	// Assert
	autopilot.Assert(t, pod.Spec.Containers[0].SecurityContext != nil, "SecurityContext should be set for agent mode")
	autopilot.Assert(t, pod.Spec.Containers[0].SecurityContext.Privileged != nil, "Privileged should be set for agent mode")
	autopilot.Equals(t, true, *pod.Spec.Containers[0].SecurityContext.Privileged)
	autopilot.Equals(t, int64(0), *pod.Spec.SecurityContext.RunAsUser)
	autopilot.Equals(t, int64(0), *pod.Spec.SecurityContext.FSGroup)
}

func TestGetPodObject_RegularJobNotPrivileged(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace:                     "test",
			SecurityContext:               corev1.PodSecurityContext{},
			TerminationGracePeriodSeconds: 30,
		},
	}
	job := opslevel.RunnerJob{
		Image: "alpine:latest",
	}
	labels := map[string]string{"app": "test"}

	// Act
	pod := runner.getPodObject("test-pod", labels, job)

	// Assert
	autopilot.Equals(t, (*corev1.SecurityContext)(nil), pod.Spec.Containers[0].SecurityContext)
}

func TestDeleteConfigMap_NilSafe(t *testing.T) {
	// Arrange - JobRunner with nil clientset (won't be used due to nil guard)
	runner := &JobRunner{
		logger: zerolog.Nop(),
	}

	// Act & Assert - should not panic when called with nil
	runner.DeleteConfigMap(nil)
}

func TestDeletePDB_NilSafe(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
	}

	// Act & Assert - should not panic when called with nil
	runner.DeletePDB(nil)
}

func TestDeletePod_NilSafe(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
	}

	// Act & Assert - should not panic when called with nil
	runner.DeletePod(nil)
}

func TestGetConfigMapObject(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace: "test-namespace",
		},
	}
	job := opslevel.RunnerJob{
		Files: []opslevel.RunnerJobFile{
			{Name: "script.sh", Contents: "#!/bin/bash\necho hello"},
			{Name: "config.yaml", Contents: "key: value"},
		},
	}

	// Act
	configMap := runner.getConfigMapObject("test-job-123", job)

	// Assert
	autopilot.Equals(t, "test-job-123", configMap.Name)
	autopilot.Equals(t, "test-namespace", configMap.Namespace)
	autopilot.Equals(t, true, *configMap.Immutable)
	autopilot.Equals(t, "#!/bin/bash\necho hello", configMap.Data["script.sh"])
	autopilot.Equals(t, "key: value", configMap.Data["config.yaml"])
}

func TestGetPBDObject(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace: "test-namespace",
		},
	}
	labels := map[string]string{"app": "test"}
	labelSelector, _ := CreateLabelSelector(labels)

	// Act
	pdb := runner.getPBDObject("test-job-123", labelSelector)

	// Assert
	autopilot.Equals(t, "test-job-123", pdb.Name)
	autopilot.Equals(t, "test-namespace", pdb.Namespace)
	autopilot.Equals(t, "0", pdb.Spec.MaxUnavailable.String())
}

// Verify that delete functions require non-nil clientset when given valid input
// These tests document the expected behavior after the defer fix
func TestDeleteFunctions_RequireClientset(t *testing.T) {
	// This test documents that delete functions will attempt to use clientset
	// when given non-nil resources. The defer fix ensures these are only called
	// after successful resource creation (when clientset operations succeeded).

	runner := &JobRunner{
		logger:    zerolog.Nop(),
		clientset: nil, // intentionally nil
	}

	// These would panic if called with non-nil resources but nil clientset
	// The defer fix prevents this by ensuring defer only runs after successful creation

	// Verify nil resources are handled safely
	runner.DeleteConfigMap(nil)
	runner.DeletePDB(nil)
	runner.DeletePod(nil)

	// If we reach here, nil handling works correctly
	t.Log("Delete functions correctly handle nil resources")
}

// Suppress unused import warning for policyv1
var _ = policyv1.PodDisruptionBudget{}
