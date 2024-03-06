package main

import (
	"github.com/opslevel/opslevel-runner/cmd"
	"github.com/opslevel/opslevel-runner/pkg"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	pkg.ImageTagVersion = version
	cmd.Execute(version, commit, date)
}
