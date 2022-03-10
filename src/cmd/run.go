package cmd

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// previewCmd represents the preview command
var runCmd = &cobra.Command{
	Use: "run",
	Run: doRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func doRun(cmd *cobra.Command, args []string) {
	runner, err := NewJobRunner()
	cobra.CheckErr(err)

	pod, err := runner.CreatePod(getPodObject())
	cobra.CheckErr(err)

	// TODO: check if pod is ready in polling loop with timeout
	time.Sleep(35 * time.Second)

	stdout, stderr, err := runner.RunWithFullOutput(pod, "busybox", "/bin/sh", "-c", "ls -al")
	cobra.CheckErr(err)
	log.Info().Msg(stdout)
	log.Error().Msg(stderr)
}

func getPodObject() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("opslevel-job-%d", time.Now().Unix()),
			Namespace: "default",
			Labels: map[string]string{
				"app": "demo",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "busybox",
					Image:           "busybox",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command: []string{
						"sleep",
						"3600",
					},
				},
			},
		},
	}
}
