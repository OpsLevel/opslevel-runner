package main

import (
	"github.com/opslevel/opslevel-runner/cmd"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	cmd.Execute()
}
