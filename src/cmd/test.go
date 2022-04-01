package cmd

import (
	"fmt"
	"os"

	"github.com/opslevel/opslevel-go"
	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var jobFile string

// previewCmd represents the preview command
var testCmd = &cobra.Command{
	Use:  "test",
	RunE: doTest,
}

func init() {
	rootCmd.AddCommand(testCmd)

	testCmd.PersistentFlags().StringVarP(&jobFile, "file", "f", ".", "File to read data from. If '-' then reads from stdin. Defaults to read from './job.yaml'")
	viper.BindPFlags(testCmd.Flags())
}

func doTest(cmd *cobra.Command, args []string) error {
	job, err := readJobInput()
	if err != nil {
		return err
	}
	if job.Id == nil {
		job.Id = "1"
	}

	runner, err := pkg.NewJobRunner()
	cobra.CheckErr(err)

	return runner.Run(*job)
}

func readJobInput() (*opslevel.RunnerJob, error) {
	if jobFile == "" {
		return nil, fmt.Errorf("please specify a job file")
	}
	if jobFile == "-" {
		viper.SetConfigType("yaml")
		viper.ReadConfig(os.Stdin)
	} else if jobFile == "." {
		viper.SetConfigFile("./job.yaml")
	} else {
		viper.SetConfigFile(jobFile)
	}
	err := viper.ReadInConfig()
	if err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}
	job := &opslevel.RunnerJob{}
	viper.Unmarshal(&job)
	return job, nil
}
