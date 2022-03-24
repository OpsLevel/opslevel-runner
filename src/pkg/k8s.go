package pkg

import (
	"context"
	"fmt"
	"io"
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

	"github.com/opslevel/opslevel-go"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func getPodEnv(configs []opslevel.JobVariable) []corev1.EnvVar {
	output := []corev1.EnvVar{}
	for _, config := range configs {
		output = append(output, corev1.EnvVar{
			Name:  config.Key,
			Value: config.Value,
		})
	}
	return output
}

func getPodObject(job opslevel.Job) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("opslevel-job-%s-%d", job.Id, time.Now().Unix()),
			Namespace: "default",
			Labels: map[string]string{
				"app": "demo",
			},
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
					Env: getPodEnv(job.Variables),
				},
			},
		},
	}
}

type JobConfig struct {
	Command       []string
	Namespace     string
	PodName       string
	ContainerName string
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
}

type JobRunner struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
}

func (s *JobRunner) Run(job opslevel.Job) error {
	id := job.Id.(string)
	// TODO: manage pods based on image for re-use?
	pod, err := s.CreatePod(getPodObject(job))
	cobra.CheckErr(err)

	// NOTE: do not use cobra.CheckErr after this point because this defer will never happen because os.Exit(1)
	// TODO: if we reuse pods then delete should not happen
	defer s.DeletePod(pod)

	// TODO: configurable timeout
	timeout := time.Second * time.Duration(viper.GetInt("pod-max-wait"))
	waitErr := s.WaitForPod(pod, timeout)
	if waitErr != nil {
		// TODO: Stream error back to OpsLevel for JobId
		// TODO: get pod status or status message?
		log.Error().Err(waitErr).Msgf("[%s] pod was not ready in %v", id, timeout)
		return nil
	}
	var stdout, stderr SafeBuffer
	// TODO: this log streamer should probably be used for All "job" logging to capture errors
	writer := NewOpsLevelLogWriter(id, time.Second*time.Duration(viper.GetInt("pod-log-max-interval")), viper.GetInt("pod-log-max-size"))
	streamer := NewLogStreamer(log.Logger, &stdout, &stderr)
	// TODO: Cleanup this streamer when run is a long lived process?
	go streamer.Run(id)

	working_directory := fmt.Sprintf("/jobs/%s/", id)
	// Use Per Job directory?
	commands := append([]string{fmt.Sprintf("mkdir -p %s", working_directory), fmt.Sprintf("cd %s", working_directory)}, job.Commands...)
	runErr := s.Exec(&stdout, &stderr, pod, pod.Spec.Containers[0].Name, viper.GetString("pod-shell"), "-e", "-c", strings.Join(commands, ";\n"))
	if runErr != nil {
		// TODO: Stream Error back to OpsLevel for JobId
		log.Error().Err(runErr).Msgf("[%s] %s", id, strings.TrimSuffix(stderr.String(), "\n"))
		return nil
	}

	// wait for buffer to empty ...
	for len(stdout.String()) > 0 {
		time.Sleep(time.Millisecond * 200)
	}
	// we need to flush the writer when the job is over - not sure this is the best way
	writer.Emit()

	return nil
}

func NewJobRunner() (*JobRunner, error) {
	config, err := getKubernetesConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := getKubernetesClientset()
	if err != nil {
		return nil, err
	}
	return &JobRunner{
		config:    config,
		clientset: clientset,
	}, nil
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

func (r *JobRunner) ExecWithConfig(config JobConfig) error {

	req := r.clientset.CoreV1().RESTClient().Post().
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
	log.Trace().Msgf("ExecWithOptions: execute(POST %s)", req.URL())
	exec, err := remotecommand.NewSPDYExecutor(r.config, "POST", req.URL())
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

func (r *JobRunner) Exec(stdout, stderr io.Writer, pod *corev1.Pod, containerName string, cmd ...string) error {
	return r.ExecWithConfig(JobConfig{
		Command:       cmd,
		Namespace:     pod.Namespace,
		PodName:       pod.Name,
		ContainerName: containerName,
		Stdin:         nil,
		Stdout:        stdout,
		Stderr:        stderr,
	})
}

func (r *JobRunner) CreatePod(podConfig *corev1.Pod) (*corev1.Pod, error) {
	log.Info().Msgf("Creating pod %s/%s ...", podConfig.Namespace, podConfig.Name)
	return r.clientset.CoreV1().Pods(podConfig.Namespace).Create(context.TODO(), podConfig, metav1.CreateOptions{})
}

func (r *JobRunner) WaitForPod(podConfig *corev1.Pod, timeout time.Duration) error {
	log.Info().Msgf("Waiting for pod %s/%s to be ready in %s ...", podConfig.Namespace, podConfig.Name, timeout)
	return wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		// TODO: progress bar?

		pod, err := r.clientset.CoreV1().Pods(podConfig.Namespace).Get(context.TODO(), podConfig.Name, metav1.GetOptions{})
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

func (r *JobRunner) DeletePod(podConfig *corev1.Pod) error {
	log.Info().Msgf("Deleting pod %s/%s ...", podConfig.Namespace, podConfig.Name)
	return r.clientset.CoreV1().Pods(podConfig.Namespace).Delete(context.TODO(), podConfig.Name, metav1.DeleteOptions{})
}
