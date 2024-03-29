package cmd

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/rs/zerolog/log"

	"github.com/spf13/cobra"
)

type Build struct {
	Version         string          `json:"version,omitempty"`
	Commit          string          `json:"git,omitempty"`
	GoInfo          GoInfo          `json:"go,omitempty"`
	OpslevelVersion OpslevelVersion `json:"opslevel,omitempty"`
}

type OpslevelVersion struct {
	Commit  string `json:"app_commit,omitempty"`
	Date    string `json:"app_date,omitempty"`
	Version string `json:"app_version,omitempty"`
}

type GoInfo struct {
	Version  string `json:"version,omitempty"`
	Compiler string `json:"compiler,omitempty"`
	OS       string `json:"os,omitempty"`
	Arch     string `json:"arch,omitempty"`
}

var (
	version = "development"
	commit  = "none"
	date    = "unknown"
	build   Build
)

func initBuild() {
	build.Version = version
	if len(commit) >= 12 {
		build.Commit = commit[:12]
	} else {
		build.Commit = commit
	}

	build.GoInfo = getGoInfo()
	build.OpslevelVersion = getOpslevelVersion()
}

func getGoInfo() GoInfo {
	return GoInfo{
		Version:  runtime.Version(),
		Compiler: runtime.Compiler,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
	}
}

func getOpslevelVersion() OpslevelVersion {
	opslevelVersion := OpslevelVersion{Date: date}
	_, err := pkg.NewRestClient().R().SetResult(&opslevelVersion).Get("api/ping")
	cobra.CheckErr(err)

	if len(opslevelVersion.Commit) >= 12 {
		opslevelVersion.Commit = opslevelVersion.Commit[:12]
	}

	return opslevelVersion
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print version information`,
	RunE:  runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func logVersion() {
	initBuild()
	log.Info().Msgf(
		"Runner Version: %s-%s-%s",
		build.OpslevelVersion.Version,
		build.OpslevelVersion.Commit,
		build.OpslevelVersion.Date,
	)
}

func runVersion(cmd *cobra.Command, args []string) error {
	initBuild()
	versionInfo, err := json.MarshalIndent(build, "", "    ")
	if err != nil {
		return err
	}
	fmt.Println(string(versionInfo))
	return nil
}
