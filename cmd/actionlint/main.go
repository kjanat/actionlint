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
	cmd := cli.Command{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(cmd.Main(os.Args))
}
