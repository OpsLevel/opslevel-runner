package pkg

import (
	"testing"

	"github.com/opslevel/opslevel-go/v2024"
	"github.com/rocktavious/autopilot/v2023"
	"github.com/rs/zerolog"
	corev1 "k8s.io/api/core/v1"
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
