// Package githubaction adapts the ordinary actionlint binary to the GitHub
// Actions input, annotation, and output protocol.
package githubaction

import (
	"io"

	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

// Main runs the GitHub Action adapter using the action's input environment.
func Main(env func(string) string, stdout io.Writer, acquisition plugininstall.Acquisition) int {
	a := &action{
		args:        environmentArgs(env),
		stdout:      stdout,
		env:         env,
		lint:        runLinter,
		newID:       newDelimiter,
		timeout:     lintTimeout,
		acquisition: acquisition,
	}
	return a.run()
}

func environmentArgs(env func(string) string) []string {
	args := make([]string, 1, 8)
	args[0] = "actionlint -github-action"
	for _, input := range []struct{ name, fallback string }{
		{"FILES", ""},
		{"FORMAT", "github"},
		{"IGNORE", ""},
		{"CONFIG-FILE", ""},
		{"WORKING-DIRECTORY", "."},
		{"OUTPUT-FILE", ""},
		{"FAIL-ON-ERROR", "true"},
	} {
		value := env("INPUT_" + input.name)
		if value == "" {
			value = input.fallback
		}
		args = append(args, value)
	}
	return args
}
