// Package githubaction adapts the ordinary actionlint binary to the GitHub
// Actions input, annotation, and output protocol.
package githubaction

import (
	"io"
)

// Main runs the GitHub Action adapter using the action's input environment.
func Main(env func(string) string, stdout io.Writer) int {
	a := &action{
		args:    environmentArgs(env),
		stdout:  stdout,
		env:     env,
		lint:    runLinter,
		newID:   newDelimiter,
		timeout: lintTimeout,
	}
	return a.run()
}

func environmentArgs(env func(string) string) []string {
	args := make([]string, 1, 10)
	args[0] = "actionlint -github-action"
	for _, input := range []struct{ name, fallback string }{
		{"FILES", ""}, {"FORMAT", "github"}, {"IGNORE", ""}, {"CONFIG-FILE", ""},
		{"SHELLCHECK", "true"}, {"PYFLAKES", "true"}, {"WORKING-DIRECTORY", "."},
		{"OUTPUT-FILE", ""}, {"FAIL-ON-ERROR", "true"},
	} {
		value := env("INPUT_" + input.name)
		if value == "" {
			value = input.fallback
		}
		args = append(args, value)
	}
	return args
}
