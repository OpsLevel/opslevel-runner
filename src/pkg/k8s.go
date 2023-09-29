package pkg

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/intstr"

	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/opslevel/opslevel-go/v2023"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

type JobConfig struct {
	Command       []string
	Namespace     string
	PodName       string
	ContainerName string
	Stdin         io.Reader
	Stdout        *SafeBuffer
	Stderr        *SafeBuffer
}

type JobRunner struct {
	runnerId     opslevel.ID
	logger       zerolog.Logger
	config       *rest.Config
	clientset    *kubernetes.Clientset
	jobPodConfig JobPodConfig
}

type JobOutcome struct {
	Message          string
	Outcome          opslevel.RunnerJobOutcomeEnum
	OutcomeVariables []opslevel.RunnerJobOutcomeVariable
}

type JobPodConfig struct {
	Namespace   string
	Lifetime    int   // in seconds
	CpuRequests int64 // in millicores!
	MemRequests int64 // in MB
	CpuLimit    int64 // in millicores!
	MemLimit    int64 // in MB
}

func NewJobRunner(runnerId opslevel.ID, logger zerolog.Logger, jobPodConfig JobPodConfig) (*JobRunner, error) {
	config, err := getKubernetesConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := getKubernetesClientset()
	if err != nil {
		return nil, err
	}
	return &JobRunner{
		runnerId:     runnerId,
		logger:       logger,
		config:       config,
		clientset:    clientset,
		jobPodConfig: jobPodConfig,
	}, nil
}

func (s *JobRunner) getPodEnv(configs []opslevel.RunnerJobVariable) []corev1.EnvVar {
	output := []corev1.EnvVar{}
	for _, config := range configs {
		output = append(output, corev1.EnvVar{
			Name:  config.Key,
			Value: config.Value,
		})
	}
	return output
}

func (s *JobRunner) getConfigMapObject(identifier string, job opslevel.RunnerJob) *corev1.ConfigMap {
	data := map[string]string{}
	for _, file := range job.Files {
		data[file.Name] = file.Contents
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identifier,
			Namespace: s.jobPodConfig.Namespace,
		},
		Immutable: opslevel.Bool(true),
		Data:      data,
	}
}

func (s *JobRunner) getPBDObject(identifier string, selector *metav1.LabelSelector) *policyv1.PodDisruptionBudget {
	maxUnavailable := intstr.Parse("0")
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identifier,
			Namespace: s.jobPodConfig.Namespace,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector:       selector,
		},
	}
}

func executable() *int32 {
	value := int32(511)
	return &value
}

func (s *JobRunner) getPodObject(identifier string, labels map[string]string, job opslevel.RunnerJob) *corev1.Pod {
	// TODO: Allow configuration of PullPolicy
	// TODO: Allow configuration of Labels
	// TODO: Allow configuration of Annotations
	// TODO: Allow configuration of Pod Command
	// TODO: Allow configuration of TerminationGracePeriodSeconds
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identifier,
			Namespace: s.jobPodConfig.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &[]int64{5}[0],
			RestartPolicy:                 corev1.RestartPolicyNever,
			InitContainers: []corev1.Container{
				{
					Name:            "helper",
					Image:           "public.ecr.aws/opslevel/opslevel-runner:v2023.9.29",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command: []string{
						"cp",
						"/opslevel-runner",
						"/mount",
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							ReadOnly:  false,
							MountPath: "/mount",
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "job",
					Image:           job.Image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command: []string{
						"/bin/sh",
						"-c",
						fmt.Sprintf("sleep %d", s.jobPodConfig.Lifetime),
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewMilliQuantity(s.jobPodConfig.CpuRequests, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(s.jobPodConfig.MemRequests*1024*1024, resource.BinarySI),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewMilliQuantity(s.jobPodConfig.CpuLimit, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(s.jobPodConfig.MemLimit*1024*1204, resource.BinarySI),
						},
					},
					Env: s.getPodEnv(job.Variables),
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "scripts",
							ReadOnly:  true,
							MountPath: "/opslevel",
						},
						{
							Name:      "shared",
							ReadOnly:  true,
							MountPath: "/mount",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "scripts",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: identifier,
							},
							DefaultMode: executable(),
						},
					},
				},
				{
					Name: "shared",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
		},
	}
}

// TODO: Remove all usages of "Viper" they should be passed in at JobRunner configuraiton time
func (s *JobRunner) Run(job opslevel.RunnerJob, stdout, stderr *SafeBuffer) JobOutcome {
	id := string(job.Id)
	identifier := fmt.Sprintf("opslevel-job-%s-%d", job.Number(), time.Now().Unix())
	runnerIdentifier := fmt.Sprintf("runner-%s", s.runnerId)
	labels := map[string]string{
		"app.kubernetes.io/instance":   identifier,
		"app.kubernetes.io/managed-by": runnerIdentifier,
	}
	labelSelector, err := CreateLabelSelector(labels)
	if err != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create label selector REASON: %s", err),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}
	// TODO: manage pods based on image for re-use?
	cfgMap, err := s.CreateConfigMap(s.getConfigMapObject(identifier, job))
	defer s.DeleteConfigMap(cfgMap) // TODO: if we reuse pods then delete should not happen?
	if err != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create configmap REASON: %s", err),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}

	pdb, err := s.CreatePDB(s.getPBDObject(identifier, labelSelector))
	defer s.DeletePDB(pdb) // TODO: if we reuse pods then delete should not happen?
	if err != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create pod disruption budget REASON: %s", err),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}

	pod, err := s.CreatePod(s.getPodObject(identifier, labels, job))
	defer s.DeletePod(pod) // TODO: if we reuse pods then delete should not happen
	if err != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create pod REASON: %s", err),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}

	timeout := time.Second * time.Duration(viper.GetInt("job-pod-max-wait"))
	waitErr := s.WaitForPod(pod, timeout)
	if waitErr != nil {
		// TODO: get pod status or status message?
		return JobOutcome{
			Message: fmt.Sprintf("pod was not ready in %v REASON: %s", timeout, waitErr),
			Outcome: opslevel.RunnerJobOutcomeEnumPodTimeout,
		}
	}

	working_directory := fmt.Sprintf("/jobs/%s/", id)
	commands := append([]string{fmt.Sprintf("mkdir -p %s", working_directory), fmt.Sprintf("cd %s", working_directory), "set -xv"}, job.Commands...)
	runErr := s.Exec(stdout, stderr, pod, pod.Spec.Containers[0].Name, viper.GetString("job-pod-shell"), "-e", "-c", strings.Join(commands, ";\n"))
	if runErr != nil {
		return JobOutcome{
			Message: fmt.Sprintf("pod execution failed REASON: %s %s", strings.TrimSuffix(stderr.String(), "\n"), runErr),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}

	return JobOutcome{
		Message: "",
		Outcome: opslevel.RunnerJobOutcomeEnumSuccess,
	}
}

func CreateLabelSelector(labels map[string]string) (*metav1.LabelSelector, error) {
	var selectors []string
	for key, value := range labels {
		selectors = append(selectors, fmt.Sprintf("%s=%s", key, value))
	}
	labelSelector, err := metav1.ParseToLabelSelector(strings.Join(selectors, ","))
	return labelSelector, err
}

func getKubernetesClientset() (*kubernetes.Clientset, error) {
	config, err := getKubernetesConfig()
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func getKubernetesConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
	config.Timeout = time.Second * time.Duration(viper.GetInt("job-pod-exec-max-wait"))
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (s *JobRunner) ExecWithConfig(config JobConfig) error {
	req := s.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(config.PodName).
		Namespace(config.Namespace).
		SubResource("exec").
		Param("container", config.ContainerName)
	req.VersionedParams(&corev1.PodExecOptions{
		Container: config.ContainerName,
		Command:   config.Command,
		Stdin:     config.Stdin != nil,
		Stdout:    config.Stdout != nil,
		Stderr:    config.Stderr != nil,
		TTY:       false,
	}, scheme.ParameterCodec)
	s.logger.Debug().Msgf("Execing pod %s/%s ...", config.Namespace, config.PodName)
	s.logger.Trace().Msgf("ExecWithOptions: execute(POST %s)", req.URL())
	exec, err := remotecommand.NewSPDYExecutor(s.config, "POST", req.URL())
	if err != nil {
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  config.Stdin,
		Stdout: config.Stdout,
		Stderr: config.Stderr,
		Tty:    false,
	})
}

func (s *JobRunner) Exec(stdout, stderr *SafeBuffer, pod *corev1.Pod, containerName string, cmd ...string) error {
	return s.ExecWithConfig(JobConfig{
		Command:       cmd,
		Namespace:     pod.Namespace,
		PodName:       pod.Name,
		ContainerName: containerName,
		Stdin:         nil,
		Stdout:        stdout,
		Stderr:        stderr,
	})
}

func (s *JobRunner) CreateConfigMap(config *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	s.logger.Trace().Msgf("Creating configmap %s/%s ...", config.Namespace, config.Name)
	return s.clientset.CoreV1().ConfigMaps(config.Namespace).Create(context.TODO(), config, metav1.CreateOptions{})
}

func (s *JobRunner) CreatePDB(config *policyv1.PodDisruptionBudget) (*policyv1.PodDisruptionBudget, error) {
	s.logger.Trace().Msgf("Creating pod disruption budget %s/%s ...", config.Namespace, config.Name)
	return s.clientset.PolicyV1().PodDisruptionBudgets(config.Namespace).Create(context.TODO(), config, metav1.CreateOptions{})
}

func (s *JobRunner) CreatePod(config *corev1.Pod) (*corev1.Pod, error) {
	s.logger.Trace().Msgf("Creating pod %s/%s ...", config.Namespace, config.Name)
	return s.clientset.CoreV1().Pods(config.Namespace).Create(context.TODO(), config, metav1.CreateOptions{})
}

func (s *JobRunner) WaitForPod(podConfig *corev1.Pod, timeout time.Duration) error {
	s.logger.Debug().Msgf("Waiting for pod %s/%s to be ready in %s ...", podConfig.Namespace, podConfig.Name, timeout)
	return wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		pod, err := s.clientset.CoreV1().Pods(podConfig.Namespace).Get(context.TODO(), podConfig.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return true, nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return false, fmt.Errorf("pod ran to completion")
		}
		return false, nil
	})
}

func (s *JobRunner) DeleteConfigMap(config *corev1.ConfigMap) error {
	s.logger.Trace().Msgf("Deleting configmap %s/%s ...", config.Namespace, config.Name)
	return s.clientset.CoreV1().ConfigMaps(config.Namespace).Delete(context.TODO(), config.Name, metav1.DeleteOptions{})
}

func (s *JobRunner) DeletePDB(config *policyv1.PodDisruptionBudget) error {
	s.logger.Trace().Msgf("Deleting configmap %s/%s ...", config.Namespace, config.Name)
	return s.clientset.PolicyV1().PodDisruptionBudgets(config.Namespace).Delete(context.TODO(), config.Name, metav1.DeleteOptions{})
}

func (s *JobRunner) DeletePod(config *corev1.Pod) error {
	s.logger.Trace().Msgf("Deleting pod %s/%s ...", config.Namespace, config.Name)
	return s.clientset.CoreV1().Pods(config.Namespace).Delete(context.TODO(), config.Name, metav1.DeleteOptions{})
}
