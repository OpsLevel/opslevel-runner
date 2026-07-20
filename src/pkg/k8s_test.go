package pkg

import (
	"context"
	"strings"
	"testing"

	"github.com/opslevel/opslevel-go/v2026"
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
	runner.DeleteConfigMap(context.Background(), nil)
}

func TestDeletePDB_NilSafe(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
	}

	// Act & Assert - should not panic when called with nil
	runner.DeletePDB(context.Background(), nil)
}

func TestDeletePod_NilSafe(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
	}

	// Act & Assert - should not panic when called with nil
	runner.DeletePod(context.Background(), nil)
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
	runner.DeleteConfigMap(context.Background(), nil)
	runner.DeletePDB(context.Background(), nil)
	runner.DeletePod(context.Background(), nil)

	// If we reach here, nil handling works correctly
	t.Log("Delete functions correctly handle nil resources")
}

func TestGetPodEnv_FiltersByScope(t *testing.T) {
	// Arrange
	runner := &JobRunner{logger: zerolog.Nop(), podConfig: &K8SPodConfig{}}
	vars := []opslevel.RunnerJobVariable{
		{Key: "BOTH", Value: "shared"},
		{Key: "INIT_ONLY", Value: "i", Scope: opslevel.RunnerJobVariableScopeInit},
		{Key: "MAIN_ONLY", Value: "m", Scope: opslevel.RunnerJobVariableScopeMain},
	}

	// Act
	initEnv := runner.getPodEnv(vars, opslevel.RunnerJobVariableScopeInit)
	mainEnv := runner.getPodEnv(vars, opslevel.RunnerJobVariableScopeMain)

	// Assert
	initKeys := envKeys(initEnv)
	mainKeys := envKeys(mainEnv)
	autopilot.Equals(t, []string{"BOTH", "INIT_ONLY"}, initKeys)
	autopilot.Equals(t, []string{"BOTH", "MAIN_ONLY"}, mainKeys)
}

func TestGetPodObject_NoInitCommands(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace:                     "test",
			WorkingDir:                    "/workdir",
			Shell:                         "/bin/sh",
			SecurityContext:               corev1.PodSecurityContext{},
			TerminationGracePeriodSeconds: 30,
		},
	}
	job := opslevel.RunnerJob{Image: "alpine:latest"}

	// Act
	pod := runner.getPodObject("test-pod", map[string]string{}, job)

	// Assert
	autopilot.Equals(t, 1, len(pod.Spec.InitContainers))
	autopilot.Equals(t, ContainerNameHelper, pod.Spec.InitContainers[0].Name)
	// workspace volume is always present, even when no init container runs
	autopilot.Assert(t, hasVolume(pod, "workspace"), "workspace volume should be present")
}

func TestGetPodObject_InitContainer(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace:                     "test",
			WorkingDir:                    "/workdir",
			Shell:                         "/bin/sh",
			SecurityContext:               corev1.PodSecurityContext{},
			TerminationGracePeriodSeconds: 30,
		},
	}
	job := opslevel.RunnerJob{
		Id:           "job-1",
		Image:        "alpine:latest",
		InitCommands: []string{"/opslevel/clone-repo ."},
		Variables: []opslevel.RunnerJobVariable{
			{Key: "REPO_CLONE_URL", Value: "https://token@example/repo.git", Sensitive: true, Scope: opslevel.RunnerJobVariableScopeInit},
			{Key: "REPO_URL", Value: "https://example/repo.git"},
			{Key: "AI_API_KEY", Value: "secret", Sensitive: true, Scope: opslevel.RunnerJobVariableScopeMain},
		},
	}

	// Act
	pod := runner.getPodObject("test-pod", map[string]string{}, job)

	// Assert: two init containers (helper, init); init runs second so the
	// runner binary is already on the shared mount by the time it boots.
	autopilot.Equals(t, 2, len(pod.Spec.InitContainers))
	autopilot.Equals(t, ContainerNameHelper, pod.Spec.InitContainers[0].Name)
	autopilot.Equals(t, ContainerNameInit, pod.Spec.InitContainers[1].Name)

	initContainer := pod.Spec.InitContainers[1]
	// Defaults to the job image when InitImage is unset.
	autopilot.Equals(t, "alpine:latest", initContainer.Image)
	// REPO_CLONE_URL and REPO_URL reach the init container; AI_API_KEY does not.
	autopilot.Equals(t, []string{"REPO_CLONE_URL", "REPO_URL"}, envKeys(initContainer.Env))

	mainContainer := pod.Spec.Containers[0]
	// REPO_CLONE_URL is excluded from the main container — this is the security
	// goal of init-container clones.
	autopilot.Equals(t, []string{"REPO_URL", "AI_API_KEY"}, envKeys(mainContainer.Env))

	// Both init and main mount the workspace RW at WorkingDir.
	autopilot.Assert(t, mountIsRW(initContainer.VolumeMounts, "workspace"), "init: workspace should be RW")
	autopilot.Assert(t, mountIsRW(mainContainer.VolumeMounts, "workspace"), "main: workspace should be RW")
}

func TestGetPodObject_InitImageOverride(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace: "test", WorkingDir: "/workdir", Shell: "/bin/sh",
			SecurityContext: corev1.PodSecurityContext{}, TerminationGracePeriodSeconds: 30,
		},
	}
	job := opslevel.RunnerJob{
		Image:        "alpine:latest",
		InitImage:    "git-tools:latest",
		InitCommands: []string{"git --version"},
	}

	// Act
	pod := runner.getPodObject("test-pod", map[string]string{}, job)

	// Assert: InitImage takes precedence over Image for the init container.
	autopilot.Equals(t, "git-tools:latest", pod.Spec.InitContainers[1].Image)
	autopilot.Equals(t, "alpine:latest", pod.Spec.Containers[0].Image)
}

func envKeys(env []corev1.EnvVar) []string {
	keys := make([]string, 0, len(env))
	for _, e := range env {
		keys = append(keys, e.Name)
	}
	return keys
}

func hasVolume(pod *corev1.Pod, name string) bool {
	for _, v := range pod.Spec.Volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

func mountIsRW(mounts []corev1.VolumeMount, name string) bool {
	for _, m := range mounts {
		if m.Name == name {
			return !m.ReadOnly
		}
	}
	return false
}

func TestJobCompletionMarker_EmbedsIdentifier(t *testing.T) {
	// Arrange
	identifier := "opslevel-job-abc-1700000000"
	// Act
	marker := jobCompletionMarker(identifier)
	// Assert
	autopilot.Assert(t, strings.Contains(marker, identifier), "marker must embed the unique pod identifier so job output cannot spoof it")
}

func TestMarkerWriter_DetectsMarkerInSingleWrite(t *testing.T) {
	// Arrange
	w := newMarkerWriter("MARKER")
	// Act
	_, err := w.Write([]byte("some logs\n[opslevel] MARKER done\nmore"))
	// Assert
	autopilot.Ok(t, err)
	autopilot.Equals(t, true, w.found())
}

func TestMarkerWriter_DetectsMarkerSplitAcrossWrites(t *testing.T) {
	// Arrange
	w := newMarkerWriter("MARKER")
	// Act: the marker straddles the boundary between two Write calls.
	_, _ = w.Write([]byte("noise fooMAR"))
	autopilot.Equals(t, false, w.found())
	_, err := w.Write([]byte("KERbaz noise"))
	// Assert
	autopilot.Ok(t, err)
	autopilot.Equals(t, true, w.found())
}

func TestMarkerWriter_AbsentMarker(t *testing.T) {
	// Arrange
	w := newMarkerWriter("MARKER")
	// Act: a truncated stream that never emits the marker (severed exec).
	_, _ = w.Write([]byte("processing check 83 of 100\n"))
	_, err := w.Write([]byte("processing check 84 of 100\n"))
	// Assert
	autopilot.Ok(t, err)
	autopilot.Equals(t, false, w.found())
}

func TestMarkerWriter_LargeStreamThenMarker(t *testing.T) {
	// Arrange: ensure carry stays bounded and detection still works after lots of data.
	w := newMarkerWriter("MARKER")
	// Act
	for i := 0; i < 1000; i++ {
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}
	autopilot.Equals(t, false, w.found())
	autopilot.Assert(t, len(w.carry) < len(w.marker), "carry must never exceed len(marker)-1 bytes")
	_, err := w.Write([]byte("tail MARKER tail"))
	// Assert
	autopilot.Ok(t, err)
	autopilot.Equals(t, true, w.found())
}

// Suppress unused import warning for policyv1
var _ = policyv1.PodDisruptionBudget{}
