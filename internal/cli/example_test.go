package cli_test

import (
	"bytes"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func ExampleCommand() {
	var output bytes.Buffer

	cmd := cli.Command{
		Stdin: strings.NewReader(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
permissions: {}
`),
		Stdout: &output,
		Stderr: &output,
	}

	// args must start with the program name.
	status := cmd.Main([]string{"actionlint", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff", "-"})

	fmt.Println("Exited with status", status)
	// Output: Exited with status 0

	if status != 0 {
		panic("actionlint command failed: " + output.String())
	}
}
