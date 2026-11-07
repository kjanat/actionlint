package cli_test

import (
	"bytes"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func ExampleCommand() {
	// Write command output to this buffer
	var output bytes.Buffer

	// Create command instance populating stdin/stdout/stderr
	cmd := cli.Command{
		Stdin: strings.NewReader(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`),
		Stdout: &output,
		Stderr: &output,
	}

	// Run the command end-to-end. Note that given args should contain program name
	status := cmd.Main([]string{"actionlint", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff", "-"})

	fmt.Println("Exited with status", status)
	// Output: Exited with status 0

	if status != 0 {
		panic("actionlint command failed: " + output.String())
	}
}
