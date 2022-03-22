package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type JobEnvSchema struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Sensitive bool   `json:"sensitive"`
}

type JobSchema struct {
	JobId    string         `json:"job_id"`
	Image    string         `json:"image"`
	Commands []string       `json:"commands"`
	Config   []JobEnvSchema `json:"config"`
}

// previewCmd represents the preview command
var runCmd = &cobra.Command{
	Use:  "run [COMMAND]",
	Args: cobra.MaximumNArgs(1),
	Run:  doRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func doRun(cmd *cobra.Command, args []string) {
	runner, err := NewJobRunner()
	cobra.CheckErr(err)

	// Enter Forever loop
	// Poll for Jobs

	job := JobSchema{
		JobId: "1224",
		//Image: "registry.gitlab.com/jklabsinc/opslevel-containers/opslevel:main",
		Image: "alpine/curl",
		Commands: []string{
			// "export KYLE_TEST=100", // Could handle per job config like this so its not baked into the container on startup and the container could be reused
			// "pwd",
			// "echo $(curl https://www.githubstatus.com/api/v2/status.json) > data.json",
			// "ls -al",
			// "cat data.json",
			"for i in 1 2 3 4 5 6 7 8 9 10 11 12 13; do echo \"Hearthbeat $i\"; sleep 1; done",
			// "env",
			// "rm data.json",
			// "ls -la",
			// "env",
			// "for i in 1 2 3 4 5 6 7 8 9 10; do echo \"Hearthbeat $i\"; sleep 1; done",
			// "mkdir ./kyle",
			// "cd ./kyle",
			// "echo \"Hello World\" > data.json",
			// "pwd",
			// "sleep 2",
			// "cat data.json",
			// "echo $KYLE_TEST",
		},
		Config: []JobEnvSchema{
			{Key: "AWS_ACCESS_KEY", Value: "123456789", Sensitive: false},
			{Key: "AWS_SECRET_KEY", Value: "987654321", Sensitive: false},
		},
	}

	// TODO: manage pods based on image for re-use?
	pod, err := runner.CreatePod(getPodObject(job))
	cobra.CheckErr(err)

	// NOTE: do not use cobra.CheckErr after this point because this defer will never happen because os.Exit(1)
	// TODO: if we reuse pods then delete should not happen
	defer runner.DeletePod(pod)

	// TODO: configurable timeout
	timeout := time.Second * 10
	waitErr := runner.WaitForPod(pod, timeout)
	if waitErr != nil {
		// TODO: Stream error back to OpsLevel for JobId
		// TODO: get pod status or status message?
		log.Error().Err(waitErr).Msgf("[%s] pod was not ready in %v", job.JobId, timeout)
		return
	}
	// TODO: Create Job Log Streamer
	// TODO: Batch Send N lines?
	var stdout, stderr SafeBuffer
	writer := NewOpsLevelLogWriter(job.JobId, time.Second*5, 1000000)
	streamer := LogStreamer{
		logger: log.Output(&writer).With().Logger(),
		stdout: &stdout,
		stderr: &stderr,
	}
	go streamer.Run(job.JobId)

	working_directory := fmt.Sprintf("/jobs/%s/", job.JobId)
	// Use Per Job directory?
	commands := append([]string{fmt.Sprintf("mkdir -p %s", working_directory), fmt.Sprintf("cd %s", working_directory)}, job.Commands...)
	// TODO: how to determine shell - sh or bash? configurable?
	runErr := runner.Run(&stdout, &stderr, pod, "job", "/bin/sh", "-e", "-c", strings.Join(commands, ";\n"))
	if runErr != nil {
		// TODO: Stream Error back to OpsLevel for JobId
		log.Error().Err(runErr).Msgf("[%s] %s", job.JobId, strings.TrimSuffix(stderr.String(), "\n"))
		return
	}

	// wait for buffer to empty ...
	for len(stdout.String()) > 0 {
		time.Sleep(time.Millisecond * 200)
	}
	writer.Emit()
}

func getPodEnv(configs []JobEnvSchema) []corev1.EnvVar {
	output := []corev1.EnvVar{}
	for _, config := range configs {
		output = append(output, corev1.EnvVar{
			Name:  config.Key,
			Value: config.Value,
		})
	}
	return output
}

func getPodObject(job JobSchema) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("opslevel-job-%s-%d", job.JobId, time.Now().Unix()),
			Namespace: "default",
			Labels: map[string]string{
				"app": "demo",
			},
		},
		Spec: corev1.PodSpec{
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
					Env: getPodEnv(job.Config),
				},
			},
		},
	}
}
