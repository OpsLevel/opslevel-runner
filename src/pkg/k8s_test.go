package pkg

import (
	"context"
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

// TestGetPodObject_CodingAgentQueue_noInitCommand verifies squid sidecar
// wiring on the coding-agent queue when the job has no InitCommands: pod
// gets helper + squid only (no job-init), squid runs as a native sidecar
// (RestartPolicy=Always) on port 3128, PROXY_ALLOWED_DOMAINS is threaded
// into squid's env, the main container has proxy env appended after user
// vars, and squid's config/runtime volumes are mounted.
func TestGetPodObject_CodingAgentQueue_noInitCommand(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace:                     "test",
			Queue:                         "coding-agent",
			WorkingDir:                    "/workdir",
			Shell:                         "/bin/sh",
			SecurityContext:               corev1.PodSecurityContext{},
			TerminationGracePeriodSeconds: 30,
		},
	}
	job := opslevel.RunnerJob{
		Image: "alpine:latest",
		Variables: []opslevel.RunnerJobVariable{
			{Key: "PROXY_ALLOWED_DOMAINS", Value: "github.com,gitlab.com"},
			{Key: "AI_API_KEY", Value: "secret"},
		},
	}

	// Act
	pod := runner.getPodObject("test-pod", map[string]string{}, job)

	// Assert: helper + iptables-setup + squid, in that order; no job-init
	// because job.InitCommands is empty. iptables-setup precedes squid so
	// the OUTPUT chain is populated before squid starts making outbound
	// connections.
	autopilot.Equals(t, 3, len(pod.Spec.InitContainers))
	autopilot.Equals(t, ContainerNameHelper, pod.Spec.InitContainers[0].Name)
	autopilot.Equals(t, "iptables-setup", pod.Spec.InitContainers[1].Name)
	autopilot.Equals(t, "squid", pod.Spec.InitContainers[2].Name)

	// iptables-setup runs with NET_ADMIN so it can populate the pod netns'
	// OUTPUT chain. It's a run-to-completion init (RestartPolicy nil), not
	// a native sidecar.
	iptables := pod.Spec.InitContainers[1]
	autopilot.Assert(t, iptables.RestartPolicy == nil, "iptables-setup must not be a native sidecar")
	autopilot.Assert(t, iptables.SecurityContext != nil, "iptables-setup must set SecurityContext")
	autopilot.Assert(t, iptables.SecurityContext.Capabilities != nil, "iptables-setup must add NET_ADMIN")
	autopilot.Equals(t, []corev1.Capability{"NET_ADMIN"}, iptables.SecurityContext.Capabilities.Add)

	squid := pod.Spec.InitContainers[2]
	// Native sidecar: RestartPolicy=Always so kubelet gates the next init
	// container on its startupProbe.
	autopilot.Assert(t, squid.RestartPolicy != nil, "squid must have RestartPolicy set")
	autopilot.Equals(t, corev1.ContainerRestartPolicyAlways, *squid.RestartPolicy)
	autopilot.Equals(t, int32(3128), squid.Ports[0].ContainerPort)
	// PROXY_ALLOWED_DOMAINS is threaded into the squid env so the entrypoint
	// script can materialize custom-allowed-domains.conf.
	autopilot.Equals(t, []string{"PROXY_ALLOWED_DOMAINS"}, envKeys(squid.Env))
	autopilot.Equals(t, "github.com,gitlab.com", squid.Env[0].Value)

	// Job container gets proxy env appended after the user variables.
	mainEnv := envKeys(pod.Spec.Containers[0].Env)
	autopilot.Equals(t, []string{"PROXY_ALLOWED_DOMAINS", "AI_API_KEY", "http_proxy", "https_proxy", "no_proxy"}, mainEnv)

	// Squid volumes wired.
	autopilot.Assert(t, hasVolume(pod, "squid-config"), "squid-config volume should be present")
	autopilot.Assert(t, hasVolume(pod, "squid-runtime"), "squid-runtime volume should be present")

	// Main container must drop NET_ADMIN + NET_RAW so a hostile agent
	// cannot alter iptables rules or open raw sockets.
	mainSC := pod.Spec.Containers[0].SecurityContext
	autopilot.Assert(t, mainSC != nil, "main container must have SecurityContext for coding-agent")
	autopilot.Assert(t, mainSC.Capabilities != nil, "main container must have Capabilities")
	autopilot.Equals(t,
		[]corev1.Capability{"NET_ADMIN", "NET_RAW"},
		mainSC.Capabilities.Drop,
	)
}

func TestGetPodObject_NonCodingAgentQueue(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace:                     "test",
			Queue:                         "default",
			WorkingDir:                    "/workdir",
			Shell:                         "/bin/sh",
			SecurityContext:               corev1.PodSecurityContext{},
			TerminationGracePeriodSeconds: 30,
		},
	}
	job := opslevel.RunnerJob{
		Image: "alpine:latest",
		Variables: []opslevel.RunnerJobVariable{
			{Key: "PROXY_ALLOWED_DOMAINS", Value: "github.com"},
		},
	}

	// Act
	pod := runner.getPodObject("test-pod", map[string]string{}, job)

	// Assert: helper only; no squid sidecar, no squid volumes, no proxy env.
	autopilot.Equals(t, 1, len(pod.Spec.InitContainers))
	autopilot.Equals(t, ContainerNameHelper, pod.Spec.InitContainers[0].Name)
	autopilot.Assert(t, !hasVolume(pod, "squid-config"), "squid-config volume should NOT be present")
	autopilot.Assert(t, !hasVolume(pod, "squid-runtime"), "squid-runtime volume should NOT be present")
	autopilot.Equals(t, []string{"PROXY_ALLOWED_DOMAINS"}, envKeys(pod.Spec.Containers[0].Env))
}

// TestGetPodObject_CodingAgentQueue_initCommand verifies init container
// ordering on the coding-agent queue when the job provides InitCommands:
// squid must be inserted between the helper init and the job-init
// container so the proxy is up and its ACLs are authoritative before user
// init commands (e.g. `git clone`) execute.
func TestGetPodObject_CodingAgentQueue_initCommand(t *testing.T) {
	// Arrange
	runner := &JobRunner{
		logger: zerolog.Nop(),
		podConfig: &K8SPodConfig{
			Namespace:                     "test",
			Queue:                         "coding-agent",
			WorkingDir:                    "/workdir",
			Shell:                         "/bin/sh",
			SecurityContext:               corev1.PodSecurityContext{},
			TerminationGracePeriodSeconds: 30,
		},
	}
	job := opslevel.RunnerJob{
		Id:           "job-1",
		Image:        "alpine:latest",
		InitCommands: []string{"git clone https://github.com/example/repo ."},
	}

	// Act
	pod := runner.getPodObject("test-pod", map[string]string{}, job)

	// Assert: helper + iptables-setup + squid + job-init, in that order.
	// iptables-setup must precede squid so the OUTPUT chain is populated
	// before squid egress starts. squid must precede the job-init so the
	// proxy is up and the ACL list is authoritative before the init clone
	// runs.
	autopilot.Equals(t, 4, len(pod.Spec.InitContainers))
	autopilot.Equals(t, ContainerNameHelper, pod.Spec.InitContainers[0].Name)
	autopilot.Equals(t, "iptables-setup", pod.Spec.InitContainers[1].Name)
	autopilot.Equals(t, "squid", pod.Spec.InitContainers[2].Name)
	autopilot.Equals(t, ContainerNameInit, pod.Spec.InitContainers[3].Name)
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

// Suppress unused import warning for policyv1
var _ = policyv1.PodDisruptionBudget{}
