package cmd

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	CaptureStdout bool
	CaptureStderr bool
	// If false, whitespace in std{err,out} will be removed.
	PreserveWhitespace bool
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

func (r *JobRunner) RunWithConfig(config JobConfig) (string, string, error) {

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
		Stdout:    config.CaptureStdout,
		Stderr:    config.CaptureStderr,
		TTY:       false,
	}, scheme.ParameterCodec)

	var stdout, stderr bytes.Buffer
	log.Info().Msgf("ExecWithOptions: execute(POST %s)", req.URL())
	err := execute("POST", req.URL(), r.config, config.Stdin, &stdout, &stderr)
	if config.PreserveWhitespace {
		return stdout.String(), stderr.String(), err
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func (r *JobRunner) RunWithFullOutput(pod *corev1.Pod, containerName string, cmd ...string) (string, string, error) {
	return r.RunWithConfig(JobConfig{
		Command:            cmd,
		Namespace:          pod.Namespace,
		PodName:            pod.Name,
		ContainerName:      containerName,
		Stdin:              nil,
		CaptureStdout:      true,
		CaptureStderr:      true,
		PreserveWhitespace: false,
	})
}

func execute(method string, url *url.URL, config *rest.Config, stdin io.Reader, stdout, stderr io.Writer) error {
	exec, err := remotecommand.NewSPDYExecutor(config, method, url)
	if err != nil {
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	})
}

func (r *JobRunner) CreatePod(podConfig *corev1.Pod) (*corev1.Pod, error) {
	log.Info().Msgf("Creating pod %s/%s ...", podConfig.Namespace, podConfig.Name)
	return r.clientset.CoreV1().Pods(podConfig.Namespace).Create(context.TODO(), podConfig, metav1.CreateOptions{})
}
