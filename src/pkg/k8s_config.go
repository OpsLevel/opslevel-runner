package pkg

import (
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"os"
)

type Config struct {
	Kubernetes K8SPodConfig `yaml:"kubernetes"`
}

type K8SPodConfig struct {
	Namespace                     string                      `yaml:"namespace"`
	Lifetime                      int                         `yaml:"lifetime"` // in seconds
	Shell                         string                      `yaml:"shell"`
	Annotations                   map[string]string           `yaml:"annotations"`
	Resources                     corev1.ResourceRequirements `yaml:"resources"`
	ServiceAccountName            string                      `yaml:"serviceAccountName"`
	TerminationGracePeriodSeconds int64                       `yaml:"terminationGracePeriodSeconds"`
	DNSPolicy                     corev1.DNSPolicy            `yaml:"dnsPolicy"`
	PullPolicy                    corev1.PullPolicy           `yaml:"pullPolicy"`
	SecurityContext               corev1.PodSecurityContext   `yaml:"securityContext"`
	NodeSelector                  map[string]string           `yaml:"nodeSelector"`
}

func ReadPodConfig(path string) (*K8SPodConfig, error) {
	config := Config{
		Kubernetes: K8SPodConfig{
			Namespace: viper.GetString("job-pod-namespace"),
			Lifetime:  viper.GetInt("job-pod-max-lifetime"),
			Shell:     viper.GetString("job-pod-shell"),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    *resource.NewMilliQuantity(viper.GetInt64("job-pod-requests-cpu"), resource.DecimalSI),
					corev1.ResourceMemory: *resource.NewQuantity(viper.GetInt64("job-pod-requests-memory")*1024*1024, resource.BinarySI),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    *resource.NewMilliQuantity(viper.GetInt64("job-pod-limits-cpu"), resource.DecimalSI),
					corev1.ResourceMemory: *resource.NewQuantity(viper.GetInt64("job-pod-limits-memory")*1024*1204, resource.BinarySI),
				},
			},
			TerminationGracePeriodSeconds: 5,
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
