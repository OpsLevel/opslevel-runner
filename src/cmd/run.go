package cmd

import (
	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

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
	runner, err := pkg.NewJobRunner()
	cobra.CheckErr(err)

	// Enter Forever loop
	// Poll for Jobs

	job := pkg.JobSchema{
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
		Config: []pkg.JobEnvSchema{
			{Key: "AWS_ACCESS_KEY", Value: "123456789", Sensitive: false},
			{Key: "AWS_SECRET_KEY", Value: "987654321", Sensitive: false},
		},
	}
	data, err := job.ToJson()
	cobra.CheckErr(err)
	cipher := pkg.NewCipher("123456789", "12")
	encoded, err := cipher.Encrypt(data)
	cobra.CheckErr(err)
	decoded, err := cipher.Decrypt(encoded)
	cobra.CheckErr(err)
	log.Info().Msgf(string(decoded))
	cobra.CheckErr(runner.Run(&job))
}
