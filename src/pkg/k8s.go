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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"

	"github.com/opslevel/opslevel-go/v2026"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

const (
	ContainerNameHelper = "helper"
	ContainerNameInit   = "init"
	ContainerNameJob    = "job"

	SquidProxyImage    = "ubuntu/squid:latest@sha256:6a097f68bae708cedbabd6188d68c7e2e7a38cedd05a176e1cc0ba29e3bbe029"
	SquidConfigMapName = "squid-config"
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
		// It's ok if this function panics because we wouldn't be able to run jobs anyway
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

// getPodEnv returns the env vars to inject into a container for the given
// scope. Variables with no Scope set are visible to every container; variables
// with a Scope are only visible to containers running in that scope.
func (s *JobRunner) getPodEnv(configs []opslevel.RunnerJobVariable, scope opslevel.RunnerJobVariableScope) []corev1.EnvVar {
	output := make([]corev1.EnvVar, 0)
	for _, config := range configs {
		if config.Scope != "" && config.Scope != scope {
			continue
		}
		output = append(output, corev1.EnvVar{
			Name:  config.Key,
			Value: config.Value,
		})
	}
	return output
}

func getRunnerJobVariable(vars []opslevel.RunnerJobVariable, key string) string {
	for _, v := range vars {
		if v.Key == key {
			return v.Value
		}
	}
	return ""
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
		Immutable: ptr.To(true),
		Data:      data,
	}
}

func (s *JobRunner) getPBDObject(identifier string, selector *metav1.LabelSelector) *policyv1.PodDisruptionBudget {
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
			MaxUnavailable: ptr.To(intstr.Parse("0")),
			Selector:       selector,
		},
	}
}

func executable() *int32 {
	return ptr.To(int32(511))
}

func getContainerNames(containers []corev1.Container) []string {
	names := make([]string, 0, len(containers))
	for _, container := range containers {
		names = append(names, container.Name)
	}
	return names
}

func (s *JobRunner) getPodObject(identifier string, labels map[string]string, job opslevel.RunnerJob) *corev1.Pod {
	// TODO: Allow configuration of Labels
	// TODO: Allow configuration of Pod Command

	podSecurityContext := s.podConfig.SecurityContext
	if s.podConfig.AgentMode {
		// Agent mode jobs need root user for Docker daemon
		podSecurityContext = corev1.PodSecurityContext{
			RunAsUser: ptr.To(int64(0)),
			FSGroup:   ptr.To(int64(0)),
		}
	}

	var containerSecurityContext *corev1.SecurityContext
	if s.podConfig.AgentMode {
		// Agent mode jobs need privileged mode for creating containers within container
		containerSecurityContext = &corev1.SecurityContext{
			Privileged: ptr.To(true),
		}
	}

	containers := []corev1.Container{
		{
			Name:            ContainerNameJob,
			Image:           job.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{
				"/bin/sh",
				"-c",
				fmt.Sprintf("sleep %d", s.podConfig.Lifetime),
			},
			Resources:       s.podConfig.Resources,
			Env:             s.getPodEnv(job.Variables, opslevel.RunnerJobVariableScopeMain),
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
				{
					Name:      "workspace",
					ReadOnly:  false,
					MountPath: s.podConfig.WorkingDir,
				},
			},
		},
	}

	volumes := []corev1.Volume{
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
		{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// helperContainer copies the runner binary into the shared volume. It runs
	// to completion before the main and sidecar containers start.
	helperContainer := corev1.Container{
		Name:            ContainerNameHelper,
		Image:           s.podConfig.helperImage(),
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
	}
	initContainers := []corev1.Container{helperContainer}

	// squid sidecar runs before any job-init container so the networking layer
	// (proxy + ACL list from the configmap) is up before init-clone traffic
	// starts. It's a native sidecar (init container with RestartPolicy=Always)
	// so kubelet gates the *next* init container on its TCP startupProbe.
	// working with the queue name; can't use agentMode until agentMode stops
	// implying privileged mode
	if s.podConfig.Queue == "coding-agent" {
		proxyAllowedDomains := getRunnerJobVariable(job.Variables, "PROXY_ALLOWED_DOMAINS")
		squidUID := int64(13)
		initContainers = append(initContainers, corev1.Container{
			Name:            "squid",
			Image:           SquidProxyImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			RestartPolicy:   ptr.To(corev1.ContainerRestartPolicyAlways),
			Command:         []string{"/bin/sh", "-c"},
			Args: []string{`set -eu
: > /srv/squid/custom-allowed-domains.conf
if [ -n "${PROXY_ALLOWED_DOMAINS:-}" ]; then
  echo "$PROXY_ALLOWED_DOMAINS" | tr ',' '\n' > /srv/squid/custom-allowed-domains.conf
fi
printf 'include /etc/squid/conf.d/squid.conf\npid_filename /srv/squid/squid.pid\n' > /srv/squid/squid.conf
exec squid -N -f /srv/squid/squid.conf
`},
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:  &squidUID,
				RunAsGroup: &squidUID,
			},
			Ports: []corev1.ContainerPort{
				{Name: "proxy", ContainerPort: 3128, Protocol: corev1.ProtocolTCP},
			},
			Env: []corev1.EnvVar{
				{Name: "PROXY_ALLOWED_DOMAINS", Value: proxyAllowedDomains},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "squid-config", ReadOnly: true, MountPath: "/etc/squid/conf.d"},
				{Name: "squid-runtime", ReadOnly: false, MountPath: "/srv/squid"},
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
			StartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(3128),
					},
				},
				InitialDelaySeconds: 0,
				PeriodSeconds:       1,
				TimeoutSeconds:      1,
				FailureThreshold:    5,
			},
		})

		volumes = append(volumes,
			corev1.Volume{
				Name: "squid-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: SquidConfigMapName,
						},
					},
				},
			},
			corev1.Volume{
				Name:         "squid-runtime",
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			},
		)

		// set env vars to route job container traffic through the sidecar proxy.
		proxyURL := "http://localhost:3128"
		containers[0].Env = append(containers[0].Env,
			corev1.EnvVar{Name: "http_proxy", Value: proxyURL},
			corev1.EnvVar{Name: "https_proxy", Value: proxyURL},
			corev1.EnvVar{Name: "no_proxy", Value: "localhost,127.0.0.1,::1"},
		)
	}

	if len(job.InitCommands) > 0 {
		initContainers = append(initContainers, s.getInitContainer(job, containerSecurityContext))
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
			InitContainers:                initContainers,
			Containers:                    containers,
			Volumes:                       volumes,
		},
	}
}

// getInitContainer assembles a container that runs job.InitCommands before the
// main job container starts. It shares the `workspace` emptyDir with the main
// container at WorkingDir, so anything written here (e.g. a cloned repo) is
// visible to the main container. Only variables scoped to "init" or unscoped
// reach this container — variables scoped to "main" do not.
func (s *JobRunner) getInitContainer(job opslevel.RunnerJob, securityContext *corev1.SecurityContext) corev1.Container {
	image := job.InitImage
	if image == "" {
		image = job.Image
	}
	workingDirectory := path.Join(s.podConfig.WorkingDir, string(job.Id))
	commands := append(
		[]string{
			fmt.Sprintf("mkdir -p %s", workingDirectory),
			fmt.Sprintf("cd %s", workingDirectory),
			"set -xv",
		},
		job.InitCommands...,
	)
	return corev1.Container{
		Name:            ContainerNameInit,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{
			s.podConfig.Shell,
			"-e",
			"-c",
			strings.Join(commands, ";\n"),
		},
		Resources:       s.podConfig.Resources,
		Env:             s.getPodEnv(job.Variables, opslevel.RunnerJobVariableScopeInit),
		SecurityContext: securityContext,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "scripts",
				ReadOnly:  true,
				MountPath: "/opslevel",
			},
			{
				Name:      "workspace",
				ReadOnly:  false,
				MountPath: s.podConfig.WorkingDir,
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

	jobLogger := s.logger.With().
		Str("job_id", string(job.Id)).
		Str("namespace", s.podConfig.Namespace).
		Logger()
	ctx = jobLogger.WithContext(ctx)

	jobLogger.Debug().
		Str("image", job.Image).
		Strs("commands", job.Commands).
		Int("files", len(job.Files)).
		Int("variables", len(job.Variables)).
		Msg("job input received")

	labelSelector, err := CreateLabelSelector(labels)
	if err != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create label selector REASON: %s", err),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}
	// TODO: manage pods based on image for re-use?
	cfgMap, err := s.CreateConfigMap(ctx, s.getConfigMapObject(identifier, job))
	if err != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create configmap REASON: %s", err),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}
	defer s.DeleteConfigMap(jobLogger.WithContext(context.Background()), cfgMap) // Use Background for cleanup to ensure it completes

	pdb, err := s.CreatePDB(ctx, s.getPBDObject(identifier, labelSelector))
	if err != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create pod disruption budget REASON: %s", err),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}
	defer s.DeletePDB(jobLogger.WithContext(context.Background()), pdb) // Use Background for cleanup to ensure it completes

	pod, err := s.CreatePod(ctx, s.getPodObject(identifier, labels, job))
	if err != nil {
		return JobOutcome{
			Message: fmt.Sprintf("failed to create pod REASON: %s", err),
			Outcome: opslevel.RunnerJobOutcomeEnumFailed,
		}
	}
	defer s.DeletePod(jobLogger.WithContext(context.Background()), pod) // Use Background for cleanup to ensure it completes

	timeout := time.Second * time.Duration(viper.GetInt("job-pod-max-wait"))
	waitErr := s.WaitForPod(ctx, pod, timeout)
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
	config.QPS = float32(viper.GetInt("k8s-api-qps"))
	config.Burst = viper.GetInt("k8s-api-burst")
	config.Timeout = time.Second * time.Duration(viper.GetInt("job-pod-exec-max-wait"))
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (s *JobRunner) ExecWithConfig(ctx context.Context, config JobConfig) error {
	log := zerolog.Ctx(ctx)
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
	log.Debug().
		Str("kind", "Pod").
		Str("name", config.PodName).
		Str("container", config.ContainerName).
		Msg("execing pod")
	log.Trace().Str("url", req.URL().String()).Msg("exec request")
	exec, err := remotecommand.NewSPDYExecutor(s.config, "POST", req.URL())
	if err != nil {
		log.Error().Err(err).
			Str("kind", "Pod").
			Str("name", config.PodName).
			Msg("pod exec failed")
		return err
	}
	start := time.Now()
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  config.Stdin,
		Stdout: config.Stdout,
		Stderr: config.Stderr,
		Tty:    false,
	})
	if err != nil {
		log.Error().Err(err).
			Str("kind", "Pod").
			Str("name", config.PodName).
			Int64("duration_ms", time.Since(start).Milliseconds()).
			Msg("pod exec failed")
		return err
	}
	log.Debug().
		Str("kind", "Pod").
		Str("name", config.PodName).
		Int64("duration_ms", time.Since(start).Milliseconds()).
		Msg("pod exec complete")
	return nil
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

func (s *JobRunner) CreateConfigMap(ctx context.Context, config *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	log := zerolog.Ctx(ctx)
	keys := make([]string, 0, len(config.Data))
	for k := range config.Data {
		keys = append(keys, k)
	}
	log.Debug().
		Str("kind", "ConfigMap").
		Str("name", config.Name).
		Bool("immutable", config.Immutable != nil && *config.Immutable).
		Strs("data_keys", keys).
		Msg("creating resource")

	out, err := s.clientset.CoreV1().ConfigMaps(config.Namespace).Create(ctx, config, metav1.CreateOptions{})
	if err != nil {
		log.Error().Err(err).
			Str("kind", "ConfigMap").
			Str("name", config.Name).
			Msg("create resource failed")
		return out, err
	}
	log.Debug().
		Str("kind", "ConfigMap").
		Str("name", config.Name).
		Msg("created resource")
	return out, nil
}

func (s *JobRunner) CreatePDB(ctx context.Context, config *policyv1.PodDisruptionBudget) (*policyv1.PodDisruptionBudget, error) {
	log := zerolog.Ctx(ctx)
	maxUnavailable := ""
	if config.Spec.MaxUnavailable != nil {
		maxUnavailable = config.Spec.MaxUnavailable.String()
	}
	selector := ""
	if config.Spec.Selector != nil {
		parts := make([]string, 0, len(config.Spec.Selector.MatchLabels))
		for k, v := range config.Spec.Selector.MatchLabels {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		selector = strings.Join(parts, ",")
	}
	log.Debug().
		Str("kind", "PodDisruptionBudget").
		Str("name", config.Name).
		Str("max_unavailable", maxUnavailable).
		Str("selector", selector).
		Msg("creating resource")

	out, err := s.clientset.PolicyV1().PodDisruptionBudgets(config.Namespace).Create(ctx, config, metav1.CreateOptions{})
	if err != nil {
		log.Error().Err(err).
			Str("kind", "PodDisruptionBudget").
			Str("name", config.Name).
			Msg("create resource failed")
		return out, err
	}
	log.Debug().
		Str("kind", "PodDisruptionBudget").
		Str("name", config.Name).
		Msg("created resource")
	return out, nil
}

func (s *JobRunner) CreatePod(ctx context.Context, config *corev1.Pod) (*corev1.Pod, error) {
	log := zerolog.Ctx(ctx)

	// main job container is index 0 by design
	c := config.Spec.Containers[0]

	log.Debug().
		Str("kind", "Pod").
		Str("name", config.Name).
		Str("image", c.Image).
		Int("containers", len(config.Spec.Containers)).
		Int("init_containers", len(config.Spec.InitContainers)).
		Strs("init_container_names", getContainerNames(config.Spec.InitContainers)).
		Str("cpu_request", c.Resources.Requests.Cpu().String()).
		Str("mem_request", c.Resources.Requests.Memory().String()).
		Str("cpu_limit", c.Resources.Limits.Cpu().String()).
		Str("mem_limit", c.Resources.Limits.Memory().String()).
		Bool("privileged", c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged).
		Int("env_count", len(c.Env)).
		Int("volume_count", len(config.Spec.Volumes)).
		Str("restart_policy", string(config.Spec.RestartPolicy)).
		Msg("creating resource")

	out, err := s.clientset.CoreV1().Pods(config.Namespace).Create(ctx, config, metav1.CreateOptions{})
	if err != nil {
		log.Error().Err(err).
			Str("kind", "Pod").
			Str("name", config.Name).
			Msg("create resource failed")
		return out, err
	}
	log.Debug().
		Str("kind", "Pod").
		Str("name", config.Name).
		Msg("created resource")
	return out, nil
}

func (s *JobRunner) isPodInDesiredState(podConfig *corev1.Pod) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
		pod, err := s.clientset.CoreV1().Pods(podConfig.Namespace).Get(ctx, podConfig.Name, metav1.GetOptions{})
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

func (s *JobRunner) WaitForPod(ctx context.Context, podConfig *corev1.Pod, timeout time.Duration) error {
	log := zerolog.Ctx(ctx)
	log.Debug().
		Str("kind", "Pod").
		Str("name", podConfig.Name).
		Int64("timeout_seconds", int64(timeout.Seconds())).
		Msg("waiting for pod")
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	err := wait.PollUntilContextTimeout(waitCtx, time.Second, timeout, false, s.isPodInDesiredState(podConfig))
	if err != nil {
		log.Error().Err(err).
			Str("kind", "Pod").
			Str("name", podConfig.Name).
			Int64("duration_ms", time.Since(start).Milliseconds()).
			Msg("pod not ready")
		return err
	}
	log.Debug().
		Str("kind", "Pod").
		Str("name", podConfig.Name).
		Int64("duration_ms", time.Since(start).Milliseconds()).
		Msg("pod ready")
	return nil
}

func (s *JobRunner) DeleteConfigMap(ctx context.Context, config *corev1.ConfigMap) {
	if config == nil {
		return
	}
	log := zerolog.Ctx(ctx)
	log.Debug().
		Str("kind", "ConfigMap").
		Str("name", config.Name).
		Msg("deleting resource")
	err := s.clientset.CoreV1().ConfigMaps(config.Namespace).Delete(ctx, config.Name, metav1.DeleteOptions{})
	if err != nil {
		log.Error().Err(err).
			Str("kind", "ConfigMap").
			Str("name", config.Name).
			Msg("delete resource failed")
		return
	}
	log.Debug().
		Str("kind", "ConfigMap").
		Str("name", config.Name).
		Msg("deleted resource")
}

func (s *JobRunner) DeletePDB(ctx context.Context, config *policyv1.PodDisruptionBudget) {
	if config == nil {
		return
	}
	log := zerolog.Ctx(ctx)
	log.Debug().
		Str("kind", "PodDisruptionBudget").
		Str("name", config.Name).
		Msg("deleting resource")
	err := s.clientset.PolicyV1().PodDisruptionBudgets(config.Namespace).Delete(ctx, config.Name, metav1.DeleteOptions{})
	if err != nil {
		log.Error().Err(err).
			Str("kind", "PodDisruptionBudget").
			Str("name", config.Name).
			Msg("delete resource failed")
		return
	}
	log.Debug().
		Str("kind", "PodDisruptionBudget").
		Str("name", config.Name).
		Msg("deleted resource")
}

func (s *JobRunner) DeletePod(ctx context.Context, config *corev1.Pod) {
	if config == nil {
		return
	}
	log := zerolog.Ctx(ctx)
	log.Debug().
		Str("kind", "Pod").
		Str("name", config.Name).
		Msg("deleting resource")
	err := s.clientset.CoreV1().Pods(config.Namespace).Delete(ctx, config.Name, metav1.DeleteOptions{})
	if err != nil {
		log.Error().Err(err).
			Str("kind", "Pod").
			Str("name", config.Name).
			Msg("delete resource failed")
		return
	}
	log.Debug().
		Str("kind", "Pod").
		Str("name", config.Name).
		Msg("deleted resource")
}
