package pkg

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/opslevel/opslevel-go/v2026"
	"github.com/rocktavious/autopilot/v2023"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func boolPtr(value bool) *bool {
	return &value
}

func TestReadPodConfig_ContainerSecurityContext(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := []byte(`
kubernetes:
  containerSecurityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
        - ALL
`)
	if err := os.WriteFile(configPath, configYAML, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := ReadPodConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.ContainerSecurityContext == nil {
		t.Fatal("ContainerSecurityContext should be loaded")
	}
	if config.ContainerSecurityContext.AllowPrivilegeEscalation == nil {
		t.Fatal("AllowPrivilegeEscalation should be loaded")
	}
	autopilot.Equals(t, false, *config.ContainerSecurityContext.AllowPrivilegeEscalation)
	if config.ContainerSecurityContext.Capabilities == nil {
		t.Fatal("Capabilities should be loaded")
	}
	autopilot.Equals(t, []corev1.Capability{"ALL"}, config.ContainerSecurityContext.Capabilities.Drop)
}

func TestReadPodConfig_InvalidAutomountServiceAccountToken(t *testing.T) {
	const key = "job-pod-automount-service-account-token"
	originalValue := viper.Get(key)
	viper.Set(key, "flase")
	t.Cleanup(func() { viper.Set(key, originalValue) })

	config, err := ReadPodConfig(filepath.Join(t.TempDir(), "missing.yaml"))

	if err == nil {
		t.Fatal("ReadPodConfig should reject an invalid automount value")
	}
	autopilot.Equals(t, (*K8SPodConfig)(nil), config)
	autopilot.Equals(t, `invalid job-pod-automount-service-account-token value "flase": expected true, false, or empty`, err.Error())
}

// fullyPopulatedPodConfig sets every field of K8SPodConfig to a distinctive
// non-zero value so the wiring assertions below can tell it apart from a default.
func fullyPopulatedPodConfig() *K8SPodConfig {
	runAsUser := int64(1234)
	allowPrivilegeEscalation := false
	return &K8SPodConfig{
		Namespace:  "test-namespace",
		Lifetime:   4242,
		Shell:      "/bin/bash",
		WorkingDir: "/test-workdir",
		Annotations: map[string]string{
			"test.opslevel.com/annotation": "yes",
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU: *resource.NewMilliQuantity(250, resource.DecimalSI),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU: *resource.NewMilliQuantity(500, resource.DecimalSI),
			},
		},
		ServiceAccountName:            "test-sa",
		TerminationGracePeriodSeconds: 37,
		DNSPolicy:                     corev1.DNSClusterFirstWithHostNet,
		PullPolicy:                    corev1.PullAlways,
		SecurityContext:               corev1.PodSecurityContext{RunAsUser: &runAsUser},
		ContainerSecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &allowPrivilegeEscalation,
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		NodeSelector:     map[string]string{"katacontainers.io/kata-runtime": "true"},
		AgentMode:        false,
		HelperImage:      "test-helper:1.2.3",
		Image:            "test-job-image:4.5.6",
		RuntimeClassName: "kata-qemu",
		Tolerations: []corev1.Toleration{
			{Key: "kata-runtime", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoSchedule},
		},
		ActiveDeadlineSeconds:        1200,
		AutomountServiceAccountToken: boolPtr(false),
		Labels:                       map[string]string{"platform.opslevel.com/sandbox": "true"},
	}
}

// TestK8SPodConfig_AllFieldsWired asserts that every field of K8SPodConfig has a
// visible effect on the rendered pod. Reflection over the struct fails the test
// when a field has no assertion here, so a new config field cannot be added
// without either wiring it up or documenting why it is not part of the PodSpec.
// This is what would have caught DNSPolicy sitting unread for as long as it did.
func TestK8SPodConfig_AllFieldsWired(t *testing.T) {
	assertions := map[string]func(t *testing.T, pod *corev1.Pod){
		"Namespace": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, "test-namespace", pod.Namespace)
		},
		"Lifetime": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, "sleep 4242", pod.Spec.Containers[0].Command[2])
		},
		"Annotations": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, "yes", pod.Annotations["test.opslevel.com/annotation"])
		},
		"Resources": func(t *testing.T, pod *corev1.Pod) {
			containers := append([]corev1.Container{}, pod.Spec.InitContainers...)
			containers = append(containers, pod.Spec.Containers...)
			for _, container := range containers {
				t.Run(container.Name, func(t *testing.T) {
					autopilot.Equals(t, "250m", container.Resources.Requests.Cpu().String())
					autopilot.Equals(t, "500m", container.Resources.Limits.Cpu().String())
				})
			}
		},
		"ServiceAccountName": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, "test-sa", pod.Spec.ServiceAccountName)
		},
		"TerminationGracePeriodSeconds": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, int64(37), *pod.Spec.TerminationGracePeriodSeconds)
		},
		"DNSPolicy": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, corev1.DNSClusterFirstWithHostNet, pod.Spec.DNSPolicy)
		},
		"PullPolicy": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, corev1.PullAlways, pod.Spec.InitContainers[0].ImagePullPolicy)
			autopilot.Equals(t, corev1.PullAlways, pod.Spec.Containers[0].ImagePullPolicy)
		},
		"SecurityContext": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, int64(1234), *pod.Spec.SecurityContext.RunAsUser)
		},
		"ContainerSecurityContext": func(t *testing.T, pod *corev1.Pod) {
			containers := append([]corev1.Container{}, pod.Spec.InitContainers...)
			containers = append(containers, pod.Spec.Containers...)
			for _, container := range containers {
				t.Run(container.Name, func(t *testing.T) {
					securityContext := container.SecurityContext
					if securityContext == nil {
						t.Fatal("SecurityContext should be set")
					}
					if securityContext.AllowPrivilegeEscalation == nil {
						t.Fatal("AllowPrivilegeEscalation should be set")
					}
					autopilot.Equals(t, false, *securityContext.AllowPrivilegeEscalation)
					if securityContext.Capabilities == nil {
						t.Fatal("Capabilities should be set")
					}
					autopilot.Equals(t, []corev1.Capability{"ALL"}, securityContext.Capabilities.Drop)
				})
			}
		},
		"NodeSelector": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, "true", pod.Spec.NodeSelector["katacontainers.io/kata-runtime"])
		},
		"HelperImage": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, "test-helper:1.2.3", pod.Spec.InitContainers[0].Image)
		},
		"Image": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, "test-job-image:4.5.6", pod.Spec.Containers[0].Image)
		},
		"RuntimeClassName": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Assert(t, pod.Spec.RuntimeClassName != nil, "RuntimeClassName should be set")
			autopilot.Equals(t, "kata-qemu", *pod.Spec.RuntimeClassName)
		},
		"Tolerations": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, 1, len(pod.Spec.Tolerations))
			autopilot.Equals(t, "kata-runtime", pod.Spec.Tolerations[0].Key)
		},
		"ActiveDeadlineSeconds": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Assert(t, pod.Spec.ActiveDeadlineSeconds != nil, "ActiveDeadlineSeconds should be set")
			autopilot.Equals(t, int64(1200), *pod.Spec.ActiveDeadlineSeconds)
		},
		"AutomountServiceAccountToken": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Assert(t, pod.Spec.AutomountServiceAccountToken != nil, "AutomountServiceAccountToken should be set")
			autopilot.Equals(t, false, *pod.Spec.AutomountServiceAccountToken)
		},
		"Labels": func(t *testing.T, pod *corev1.Pod) {
			autopilot.Equals(t, "true", pod.Labels["platform.opslevel.com/sandbox"])
		},
		// Fields that legitimately do not surface in the pod object itself.
		// Shell and WorkingDir shape the exec command, not the PodSpec; AgentMode
		// has dedicated coverage in TestGetPodObject_AgentModePrivileged.
		"Shell":      nil,
		"WorkingDir": nil,
		"AgentMode":  nil,
	}

	runner := &JobRunner{logger: zerolog.Nop(), podConfig: fullyPopulatedPodConfig()}
	pod := runner.getPodObject("test-pod", map[string]string{"app": "test"}, opslevel.RunnerJob{
		Image:        "from-job:latest",
		InitCommands: []string{"prepare"},
	})

	configType := reflect.TypeOf(K8SPodConfig{})
	for i := range configType.NumField() {
		name := configType.Field(i).Name
		assertion, ok := assertions[name]
		if !ok {
			t.Errorf("K8SPodConfig field %q has no wiring assertion - either wire it into the pod object or add it to the exempt list in this test with a reason", name)
			continue
		}
		if assertion == nil {
			continue
		}
		t.Run(name, func(t *testing.T) { assertion(t, pod) })
	}
}

// TestGetPodObject_NewFieldsUnsetIsBackwardsCompatible pins the pointer-type
// caveat: an unset runtime class or deadline must stay nil, since an empty
// runtime class or a zero deadline is rejected by the API server rather than
// treated as "unspecified".
func TestGetPodObject_NewFieldsUnsetIsBackwardsCompatible(t *testing.T) {
	runner := &JobRunner{
		logger:    zerolog.Nop(),
		podConfig: &K8SPodConfig{Namespace: "test"},
	}

	pod := runner.getPodObject("test-pod", map[string]string{"app": "test"}, opslevel.RunnerJob{Image: "alpine:latest"})

	autopilot.Assert(t, pod.Spec.RuntimeClassName == nil, "RuntimeClassName should stay nil when unset")
	autopilot.Assert(t, pod.Spec.ActiveDeadlineSeconds == nil, "ActiveDeadlineSeconds should stay nil when unset")
	autopilot.Assert(t, pod.Spec.AutomountServiceAccountToken == nil, "AutomountServiceAccountToken should stay nil when unset")
	autopilot.Assert(t, pod.Spec.Tolerations == nil, "Tolerations should stay nil when unset")
	containers := append([]corev1.Container{}, pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	for _, container := range containers {
		t.Run(container.Name+"SecurityContext", func(t *testing.T) {
			autopilot.Assert(t, container.SecurityContext == nil, "SecurityContext should stay nil when unset")
		})
	}
	autopilot.Equals(t, "alpine:latest", pod.Spec.Containers[0].Image)
	autopilot.Equals(t, corev1.PullIfNotPresent, pod.Spec.Containers[0].ImagePullPolicy)
}

func TestPodLabels_RunnerLabelsWinOnConflict(t *testing.T) {
	config := &K8SPodConfig{Labels: map[string]string{
		"app.kubernetes.io/instance":    "hijacked",
		"platform.opslevel.com/sandbox": "true",
	}}

	labels := config.podLabels(map[string]string{"app.kubernetes.io/instance": "opslevel-job-1"})

	autopilot.Equals(t, "opslevel-job-1", labels["app.kubernetes.io/instance"])
	autopilot.Equals(t, "true", labels["platform.opslevel.com/sandbox"])
}

func TestJobImage(t *testing.T) {
	autopilot.Equals(t, "from-job:1", (&K8SPodConfig{}).jobImage("from-job:1"))
	autopilot.Equals(t, "override:2", (&K8SPodConfig{Image: "override:2"}).jobImage("from-job:1"))
}
