package main

import (
	"os"

	"actionlint.kjanat.dev/internal/cli"

	_ "time/tzdata"
)

func main() {
	cmd := cli.Command{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(cmd.Main(os.Args))
}
