package pkg

import (
	"context"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/api/resource"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/opslevel/opslevel-go/v2022"
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
	logger    zerolog.Logger
	namespace string
	config    *rest.Config
	clientset *kubernetes.Clientset
	resources JobPodResourceDefs
}

type JobOutcome struct {
	Message          string
	Outcome          opslevel.RunnerJobOutcomeEnum
	OutcomeVariables []opslevel.RunnerJobOutcomeVariable
}

type JobPodResourceDefs struct {
	CpuReq   int64 //in millicores!
	MemReq   int64 //in MB
	CpuLimit int64 //in millicores!
	MemLimit int64 //in MB
}

func NewJobRunner(logger zerolog.Logger, namespace string, resources JobPodResourceDefs) (*JobRunner, error) {
	config, err := getKubernetesConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := getKubernetesClientset()
	if err != nil {
		return nil, err
	}
	return &JobRunner{
		logger:    logger,
		namespace: namespace,
		config:    config,
		clientset: clientset,
		resources: resources,
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
			Namespace: s.namespace,
		},
		Immutable: opslevel.Bool(true),
		Data:      data,
	}
}

func executable() *int32 {
	value := int32(511)
	return &value
}

func (s *JobRunner) getPodObject(identifier string, job opslevel.RunnerJob) *corev1.Pod {
	// TODO: Allow configuration of PullPolicy
	// TODO: Allow configuration of Labels
	// TODO: Allow configuration of Annotations
	// TODO: Allow configuration of Pod Command
	// TODO: Allow configuration of TerminationGracePeriodSeconds
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identifier,
			Namespace: s.namespace,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &[]int64{5}[0],
			Containers: []corev1.Container{
				{
					Name:            "job",
					Image:           job.Image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command: []string{
						"/bin/sh",
						"-c",
						"while :; do sleep 30; done",
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewMilliQuantity(s.resources.CpuReq, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(s.resources.MemReq, resource.BinarySI),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewMilliQuantity(s.resources.CpuLimit, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(s.resources.MemLimit, resource.BinarySI),
						},
					},
					Env: s.getPodEnv(job.Variables),
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "scripts",
							ReadOnly:  true,
							MountPath: "/opslevel",
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
			},
		},
	}
}

// TODO: Remove all usages of "Viper" they should be passed in at JobRunner configuraiton time
func (s *JobRunner) Run(job opslevel.RunnerJob, stdout, stderr *SafeBuffer) JobOutcome {
	id := job.Id.(string)
	identifier := fmt.Sprintf("opslevel-job-%s-%d", job.Number(), time.Now().Unix())
	// TODO: manage pods based on image for re-use?
	cfgMap, cfgMapErr := s.CreateConfigMap(s.getConfigMapObject(identifier, job))
	if cfgMapErr != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create configmap REASON: %s", cfgMapErr),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}

	// NOTE: do not use cobra.CheckErr after this point because this defer will never happen because os.Exit(1)
	// TODO: if we reuse pods then delete should not happen?
	defer s.DeleteConfigMap(cfgMap)

	pod, podErr := s.CreatePod(s.getPodObject(identifier, job))
	if podErr != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create pod REASON: %s", podErr),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}

	// TODO: if we reuse pods then delete should not happen
	defer s.DeletePod(pod)

	timeout := time.Second * time.Duration(viper.GetInt("pod-max-wait"))
	waitErr := s.WaitForPod(pod, timeout)
	if waitErr != nil {
		// TODO: get pod status or status message?
		return JobOutcome{
			Message: fmt.Sprintf("pod was not ready in %v REASON: %s", timeout, waitErr),
			Outcome: opslevel.RunnerJobOutcomeEnumPodTimeout,
		}
	}

	// // TODO: this log streamer should probably be used for All "job" logging to capture errors
	//writer := NewOpsLevelLogWriter(s.index, time.Second*time.Duration(viper.GetInt("pod-log-max-interval")), viper.GetInt("pod-log-max-size"))

	working_directory := fmt.Sprintf("/jobs/%s/", id)
	commands := append([]string{fmt.Sprintf("mkdir -p %s", working_directory), fmt.Sprintf("cd %s", working_directory), "set -xv"}, job.Commands...)
	runErr := s.Exec(stdout, stderr, pod, pod.Spec.Containers[0].Name, viper.GetString("pod-shell"), "-e", "-c", strings.Join(commands, ";\n"))
	if runErr != nil {
		return JobOutcome{
			Message: fmt.Sprintf("pod execution failed REASON: %s %s", strings.TrimSuffix(stderr.String(), "\n"), runErr),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}

	// // we need to flush the writer when the job is over - not sure this is the best way
	//writer.Emit()

	return JobOutcome{
		Message: "",
		Outcome: opslevel.RunnerJobOutcomeEnumSuccess,
	}
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

func (s *JobRunner) CreateConfigMap(configMapConfig *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	s.logger.Trace().Msgf("Creating configmap %s/%s ...", configMapConfig.Namespace, configMapConfig.Name)
	return s.clientset.CoreV1().ConfigMaps(configMapConfig.Namespace).Create(context.TODO(), configMapConfig, metav1.CreateOptions{})
}

func (s *JobRunner) CreatePod(podConfig *corev1.Pod) (*corev1.Pod, error) {
	s.logger.Trace().Msgf("Creating pod %s/%s ...", podConfig.Namespace, podConfig.Name)
	return s.clientset.CoreV1().Pods(podConfig.Namespace).Create(context.TODO(), podConfig, metav1.CreateOptions{})
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

func (s *JobRunner) DeleteConfigMap(configMapConfig *corev1.ConfigMap) error {
	s.logger.Trace().Msgf("Deleting configmap %s/%s ...", configMapConfig.Namespace, configMapConfig.Name)
	return s.clientset.CoreV1().ConfigMaps(configMapConfig.Namespace).Delete(context.TODO(), configMapConfig.Name, metav1.DeleteOptions{})
}

func (s *JobRunner) DeletePod(podConfig *corev1.Pod) error {
	s.logger.Trace().Msgf("Deleting pod %s/%s ...", podConfig.Namespace, podConfig.Name)
	return s.clientset.CoreV1().Pods(podConfig.Namespace).Delete(context.TODO(), podConfig.Name, metav1.DeleteOptions{})
}
