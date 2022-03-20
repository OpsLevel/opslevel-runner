package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/rs/zerolog/log"
)

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

func (r *JobRunner) RunWithConfig(config JobConfig) error {

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
	log.Info().Msgf("ExecWithOptions: execute(POST %s)", req.URL())
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

func (r *JobRunner) Run(stdout, stderr io.Writer, pod *corev1.Pod, containerName string, cmd ...string) error {
	return r.RunWithConfig(JobConfig{
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
