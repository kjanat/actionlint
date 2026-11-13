package main

import (
	"os"

	"actionlint.kjanat.dev/v2/internal/cli"
	githubaction "actionlint.kjanat.dev/v2/internal/cli/action"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	_ "time/tzdata"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-github-action-plugins" {
		os.Exit(githubaction.PluginPlan(os.Getenv, os.Stdout, os.Stderr, plugininstall.Acquisition{}))
	}
	if len(os.Args) > 1 && os.Args[1] == "-github-action" {
		os.Exit(githubaction.Main(os.Getenv, os.Stdout, plugininstall.Acquisition{}))
	}
	cmd := cli.Command{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(cmd.Main(os.Args))
}
