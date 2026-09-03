package pkg

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

type Config struct {
	Kubernetes K8SPodConfig `yaml:"kubernetes"`
}

type K8SPodConfig struct {
	Namespace                     string                      `yaml:"namespace"`
	Lifetime                      int                         `yaml:"lifetime"` // in seconds
	Shell                         string                      `yaml:"shell"`
	WorkingDir                    string                      `yaml:"workingDir"`
	Annotations                   map[string]string           `yaml:"annotations"`
	Resources                     corev1.ResourceRequirements `yaml:"resources"`
	ServiceAccountName            string                      `yaml:"serviceAccountName"`
	TerminationGracePeriodSeconds int64                       `yaml:"terminationGracePeriodSeconds"`
	DNSPolicy                     corev1.DNSPolicy            `yaml:"dnsPolicy"`
	PullPolicy                    corev1.PullPolicy           `yaml:"pullPolicy"`
	SecurityContext               corev1.PodSecurityContext   `yaml:"securityContext"`
	// ContainerSecurityContext is applied to every init and main container.
	ContainerSecurityContext *corev1.SecurityContext `yaml:"containerSecurityContext"`
	NodeSelector             map[string]string       `yaml:"nodeSelector"`
	AgentMode                bool                    `yaml:"agentMode"`
	HelperImage              string                  `yaml:"helperImage"`

	// Image overrides the image the job container runs. When empty the image
	// from the job definition is used.
	Image string `yaml:"image"`

	// RuntimeClassName selects the container runtime for the job pod, e.g.
	// "kata-qemu" for VM isolation or "gvisor". When empty the pod runs under
	// the cluster default runtime (usually runc) with no VM boundary.
	RuntimeClassName string `yaml:"runtimeClassName"`

	// Tolerations let job pods schedule onto tainted nodes - sandbox node pools
	// are typically tainted so ordinary workloads cannot land on them.
	Tolerations []corev1.Toleration `yaml:"tolerations"`

	// ActiveDeadlineSeconds bounds the lifetime of the pod itself, unlike
	// Lifetime which only bounds the job container's sleep from the inside.
	ActiveDeadlineSeconds int64 `yaml:"activeDeadlineSeconds"`

	// AutomountServiceAccountToken controls whether a Kubernetes API credential
	// is projected into the job pod. Nil leaves the namespace default in place.
	AutomountServiceAccountToken *bool `yaml:"automountServiceAccountToken"`

	// Labels are merged onto the labels the runner sets on each job pod. Runner
	// labels win on conflict since the runner selects pods by them.
	Labels map[string]string `yaml:"labels"`
}

func ReadPodConfig(path string) (*K8SPodConfig, error) {
	automountServiceAccountToken, err := automountFromViper()
	if err != nil {
		return nil, err
	}

	config := Config{
		Kubernetes: K8SPodConfig{
			Namespace:  viper.GetString("job-pod-namespace"),
			Lifetime:   viper.GetInt("job-pod-max-lifetime"),
			Shell:      viper.GetString("job-pod-shell"),
			WorkingDir: viper.GetString("job-pod-workdir"),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              *resource.NewMilliQuantity(viper.GetInt64("job-pod-requests-cpu"), resource.DecimalSI),
					corev1.ResourceMemory:           *resource.NewQuantity(viper.GetInt64("job-pod-requests-memory")*1024*1024, resource.BinarySI),
					corev1.ResourceEphemeralStorage: *resource.NewQuantity(viper.GetInt64("job-pod-requests-ephemeral-storage")*1024*1024, resource.BinarySI),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              *resource.NewMilliQuantity(viper.GetInt64("job-pod-limits-cpu"), resource.DecimalSI),
					corev1.ResourceMemory:           *resource.NewQuantity(viper.GetInt64("job-pod-limits-memory")*1024*1024, resource.BinarySI),
					corev1.ResourceEphemeralStorage: *resource.NewQuantity(viper.GetInt64("job-pod-limits-ephemeral-storage")*1024*1024, resource.BinarySI),
				},
			},
			TerminationGracePeriodSeconds: 5,
			AgentMode:                     viper.GetBool("job-agent-mode"),
			HelperImage:                   viper.GetString("job-pod-helper-image"),
			Image:                         viper.GetString("job-pod-image"),
			RuntimeClassName:              viper.GetString("job-pod-runtime-class-name"),
			ActiveDeadlineSeconds:         viper.GetInt64("job-pod-active-deadline"),
			AutomountServiceAccountToken:  automountServiceAccountToken,
		},
	}
	// Early out with viper defaults if config file doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &config.Kubernetes, nil
	}

	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(file, &config); err != nil {
		return nil, err
	}

	return &config.Kubernetes, nil
}

// automountFromViper reads the tri-state flag. It is a string rather than a bool
// because a bound bool flag always carries its default, leaving no way to tell
// "unset" from "explicitly false" - and defaulting to false would silently strip
// the token mount from every existing user. Any other non-empty value is rejected
// rather than failing open to the namespace default.
func automountFromViper() (*bool, error) {
	value := strings.TrimSpace(viper.GetString("job-pod-automount-service-account-token"))
	switch strings.ToLower(value) {
	case "":
		return nil, nil
	case "true":
		value := true
		return &value, nil
	case "false":
		value := false
		return &value, nil
	default:
		return nil, fmt.Errorf("invalid job-pod-automount-service-account-token value %q: expected true, false, or empty", value)
	}
}

// jobPullPolicy defaults to IfNotPresent rather than leaving the field empty so
// that job pods keep their historical behavior when PullPolicy is unset.
func (c *K8SPodConfig) jobPullPolicy() corev1.PullPolicy {
	if c.PullPolicy == "" {
		return corev1.PullIfNotPresent
	}
	return c.PullPolicy
}

// jobImage returns the image the job container should run - the config override
// when set, otherwise the image from the job definition.
func (c *K8SPodConfig) jobImage(jobImage string) string {
	if c.Image != "" {
		return c.Image
	}
	return jobImage
}

// runtimeClassName must be nil rather than a pointer to "" when unset - the API
// server treats an empty runtime class as a request for a class named "".
func (c *K8SPodConfig) runtimeClassName() *string {
	if c.RuntimeClassName == "" {
		return nil
	}
	return &c.RuntimeClassName
}

// activeDeadlineSeconds must be nil rather than a pointer to 0 when unset - a
// zero deadline is rejected, it does not mean "unspecified".
func (c *K8SPodConfig) activeDeadlineSeconds() *int64 {
	if c.ActiveDeadlineSeconds <= 0 {
		return nil
	}
	return &c.ActiveDeadlineSeconds
}

// podLabels merges the configured labels under the labels the runner manages -
// the runner selects and cleans up pods by its own labels, so they must win.
func (c *K8SPodConfig) podLabels(runnerLabels map[string]string) map[string]string {
	if len(c.Labels) == 0 {
		return runnerLabels
	}
	merged := make(map[string]string, len(c.Labels)+len(runnerLabels))
	for k, v := range c.Labels {
		merged[k] = v
	}
	for k, v := range runnerLabels {
		merged[k] = v
	}
	return merged
}

func (c *K8SPodConfig) helperImage() string {
	if c.HelperImage != "" {
		return c.HelperImage
	}
	return fmt.Sprintf("public.ecr.aws/opslevel/opslevel-runner:v%s", ImageTagVersion)
}
