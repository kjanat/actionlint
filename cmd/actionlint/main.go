package main

import (
	"os"

	"actionlint.kjanat.dev/internal/cli"
	"actionlint.kjanat.dev/internal/githubaction"

	_ "time/tzdata"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-github-action" {
		os.Exit(githubaction.Main(os.Getenv, os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "-github-action-tools" {
		os.Exit(githubaction.ToolPlan(os.Getenv, os.Stdout, os.Stderr))
	}
	cmd := cli.Command{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(cmd.Main(os.Args))
}
