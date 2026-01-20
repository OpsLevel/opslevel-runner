package pkg

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/util/intstr"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/opslevel/opslevel-go/v2024"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

var (
	ImageTagVersion string
	k8sValidated    bool

	k8sClientOnce   sync.Once
	sharedK8sConfig *rest.Config
	sharedK8sClient *kubernetes.Clientset
	k8sInitError    error
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
	runnerId  string
	logger    zerolog.Logger
	config    *rest.Config
	clientset *kubernetes.Clientset
	podConfig *K8SPodConfig
}

type JobOutcome struct {
	Message          string
	Outcome          opslevel.RunnerJobOutcomeEnum
	OutcomeVariables []opslevel.RunnerJobOutcomeVariable
}

func GetSharedK8sClient() (*rest.Config, *kubernetes.Clientset, error) {
	k8sClientOnce.Do(func() {
		sharedK8sConfig, k8sInitError = GetKubernetesConfig()
		if k8sInitError != nil {
			return
		}
		sharedK8sClient, k8sInitError = kubernetes.NewForConfig(sharedK8sConfig)
	})
	if k8sInitError != nil {
		return nil, nil, k8sInitError
	}
	return sharedK8sConfig, sharedK8sClient, nil
}

func LoadK8SClient() {
	_, _, err := GetSharedK8sClient()
	if err != nil {
		cobra.CheckErr(err)
	}
	k8sValidated = true
}

func NewJobRunner(runnerId string, path string) *JobRunner {
	if !k8sValidated {
		// It's ok if this function panics because we wouldn't beable to run jobs anyway
		LoadK8SClient()
	}
	// kubernetes.Clientset is thread-safe and designed to be shared across goroutines
	config, client, _ := GetSharedK8sClient() // Already validated by LoadK8SClient
	pod, err := ReadPodConfig(path)
	if err != nil {
		panic(err)
	}
	return &JobRunner{
		runnerId:  runnerId,
		logger:    log.With().Str("runner", runnerId).Logger(),
		config:    config,
		clientset: client,
		podConfig: pod,
	}
}

func (s *JobRunner) getPodEnv(configs []opslevel.RunnerJobVariable) []corev1.EnvVar {
	output := make([]corev1.EnvVar, 0)
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
			Namespace: s.podConfig.Namespace,
			// failed to create configmap REASON: ConfigMap "opslevel-job-3163545-1734383310" is invalid: metadata.ownerReferences.uid: Invalid value: "": uid must not be empty
			//OwnerReferences: []metav1.OwnerReference{
			//	{
			//		APIVersion: "v1",
			//		Kind:       "Pod",
			//		Name:       identifier,
			//	},
			//},
		},
		Immutable: opslevel.RefOf(true),
		Data:      data,
	}
}

func (s *JobRunner) getPBDObject(identifier string, selector *metav1.LabelSelector) *policyv1.PodDisruptionBudget {
	maxUnavailable := intstr.Parse("0")
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identifier,
			Namespace: s.podConfig.Namespace,
			//OwnerReferences: []metav1.OwnerReference{
			//	{
			//		APIVersion: "v1",
			//		Kind:       "Pod",
			//		Name:       identifier,
			//	},
			//},
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
	// TODO: Allow configuration of Labels
	// TODO: Allow configuration of Pod Command

	podSecurityContext := s.podConfig.SecurityContext
	if s.podConfig.AgentMode {
		// Agent mode jobs need root user for Docker daemon
		runAsUser := int64(0)
		fsGroup := int64(0)
		podSecurityContext = corev1.PodSecurityContext{
			RunAsUser: &runAsUser,
			FSGroup:   &fsGroup,
		}
	}

	var containerSecurityContext *corev1.SecurityContext
	if s.podConfig.AgentMode {
		// Agent mode jobs need privileged mode for creating containers within container
		privileged := true
		containerSecurityContext = &corev1.SecurityContext{
			Privileged: &privileged,
		}
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        identifier,
			Namespace:   s.podConfig.Namespace,
			Labels:      labels,
			Annotations: s.podConfig.Annotations,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &s.podConfig.TerminationGracePeriodSeconds,
			RestartPolicy:                 corev1.RestartPolicyNever,
			SecurityContext:               &podSecurityContext,
			ServiceAccountName:            s.podConfig.ServiceAccountName,
			NodeSelector:                  s.podConfig.NodeSelector,
			InitContainers: []corev1.Container{
				{
					Name:            "helper",
					Image:           fmt.Sprintf("public.ecr.aws/opslevel/opslevel-runner:v%s", ImageTagVersion),
					ImagePullPolicy: s.podConfig.PullPolicy,
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
						fmt.Sprintf("sleep %d", s.podConfig.Lifetime),
					},
					Resources:       s.podConfig.Resources,
					Env:             s.getPodEnv(job.Variables),
					SecurityContext: containerSecurityContext,
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

// TODO: Remove all usages of "Viper" they should be passed in at JobRunner configuration time
func (s *JobRunner) Run(ctx context.Context, job opslevel.RunnerJob, stdout, stderr *SafeBuffer) JobOutcome {
	id := string(job.Id)
	// Once we get off "the old API" method of runner we can circle back around to this
	// and fix it to generate safe pod names since k8s has limitations.
	var identifier string
	switch viper.GetString("mode") {
	case "faktory":
		identifier = fmt.Sprintf("opslevel-job-%s-%d", job.Id, time.Now().Unix())
	case "api":
		identifier = fmt.Sprintf("opslevel-job-%s-%d", job.Number(), time.Now().Unix())
	}
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

	workingDirectory := path.Join(s.podConfig.WorkingDir, id)
	commands := append([]string{fmt.Sprintf("mkdir -p %s", workingDirectory), fmt.Sprintf("cd %s", workingDirectory), "set -xv"}, job.Commands...)
	runErr := s.Exec(ctx, stdout, stderr, pod, pod.Spec.Containers[0].Name, s.podConfig.Shell, "-e", "-c", strings.Join(commands, ";\n"))
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

func GetKubernetesConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
	config.Timeout = time.Second * time.Duration(viper.GetInt("job-pod-exec-max-wait"))
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (s *JobRunner) ExecWithConfig(ctx context.Context, config JobConfig) error {
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
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  config.Stdin,
		Stdout: config.Stdout,
		Stderr: config.Stderr,
		Tty:    false,
	})
}

func (s *JobRunner) Exec(ctx context.Context, stdout, stderr *SafeBuffer, pod *corev1.Pod, containerName string, cmd ...string) error {
	return s.ExecWithConfig(ctx, JobConfig{
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

func (s *JobRunner) isPodInDesiredState(podConfig *corev1.Pod) wait.ConditionWithContextFunc {
	return func(context.Context) (bool, error) {
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
	}
}

func (s *JobRunner) WaitForPod(podConfig *corev1.Pod, timeout time.Duration) error {
	s.logger.Debug().Msgf("Waiting for pod %s/%s to be ready in %s ...", podConfig.Namespace, podConfig.Name, timeout)
	return wait.PollUntilContextTimeout(context.Background(), time.Second, timeout, false, s.isPodInDesiredState(podConfig))
}

func (s *JobRunner) DeleteConfigMap(config *corev1.ConfigMap) {
	s.logger.Trace().Msgf("Deleting configmap %s/%s ...", config.Namespace, config.Name)
	err := s.clientset.CoreV1().ConfigMaps(config.Namespace).Delete(context.TODO(), config.Name, metav1.DeleteOptions{})
	if err != nil {
		s.logger.Error().Err(err).Msgf("received error on ConfigMap deletion")
	}
}

func (s *JobRunner) DeletePDB(config *policyv1.PodDisruptionBudget) {
	s.logger.Trace().Msgf("Deleting configmap %s/%s ...", config.Namespace, config.Name)
	err := s.clientset.PolicyV1().PodDisruptionBudgets(config.Namespace).Delete(context.TODO(), config.Name, metav1.DeleteOptions{})
	if err != nil {
		s.logger.Error().Err(err).Msgf("received error on PDB deletion")
	}
}

func (s *JobRunner) DeletePod(config *corev1.Pod) {
	s.logger.Trace().Msgf("Deleting pod %s/%s ...", config.Namespace, config.Name)
	err := s.clientset.CoreV1().Pods(config.Namespace).Delete(context.TODO(), config.Name, metav1.DeleteOptions{})
	if err != nil {
		s.logger.Error().Err(err).Msgf("received error on Pod deletion")
	}
}
