// Package githubaction adapts the ordinary actionlint binary to the GitHub
// Actions input, annotation, and output protocol.
package githubaction

import (
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// Main runs the GitHub Action adapter using the action's input environment.
func Main(env func(string) string, stdout io.Writer) int {
	for _, warning := range deprecatedInputWarnings(env) {
		_, _ = io.WriteString(stdout, "::warning::"+warning+"\n")
	}
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

// deprecatedInputWarnings warns about the retired per-tool inputs: an input
// named after a bundled tool that is false, and a true input named after a
// retired flag that a bundled provider manifest declares, which is ignored.
func deprecatedInputWarnings(env func(string) string) []string {
	manifests, err := provider.Bundled()
	if err != nil {
		// The bundled manifests are a build input that tests validate.
		panic(err)
	}
	var warnings []string
	for _, manifest := range manifests {
		id := manifest.Rule.Name
		if env(toolInput(id)) == "false" {
			warnings = append(warnings, "The "+id+" input is deprecated; use disable-tools: "+id+".")
		}
		for _, retired := range manifest.DeprecatedFlags {
			if env(toolInput(retired.Name)) == "true" {
				warnings = append(warnings, "The "+retired.Name+" input is deprecated and ignored; "+id+" runs unless disable-tools lists it.")
			}
		}
	}
	return warnings
}

// toolInput returns the environment variable that holds the action input
// named name.
func toolInput(name string) string {
	return "INPUT_" + strings.ToUpper(name)
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
