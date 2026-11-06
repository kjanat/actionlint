package scripts

import (
	"path"
	"strings"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
)

// ShellExecutableName extracts the runner's executable before its first space.
// It recognizes both path separators and a Windows .exe suffix. A wrapper such
// as "env bash" identifies env, not the program that wrapper may execute.
func ShellExecutableName(shell string) string {
	command, _, _ := strings.Cut(shell, " ")
	return strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "/"))), ".exe")
}

// DefaultShell returns the shell supplied by a job container or its runner.
// Step, job and workflow shell declarations override this default through
// execution.ResolveShell. An unknown container or empty runner shell produces
// an unknown value rather than assuming Bash.
func DefaultShell(container value.Value, runnerShell string) value.Value {
	if shell := ContainerShell(container); shell.State() != value.Missing {
		return shell
	}
	if runnerShell == "" {
		return value.DynamicValue("", value.KindUnknown, value.Origin{})
	}
	return value.YAMLScalar(runnerShell, "!!str", value.Origin{})
}

// ContainerShell resolves the known job container's default shell.
func ContainerShell(container value.Value) value.Value {
	if container.State() == value.Missing {
		return value.Value{}
	}
	if !container.Known() {
		return value.DynamicValue(container.Source(), value.KindUnknown, container.Origin())
	}
	image := container
	if container.Kind() == value.Object {
		var ok bool
		image, ok = container.Prop("image")
		if !ok {
			return value.DynamicValue("", value.KindUnknown, container.Origin())
		}
	}
	text, known := image.ScalarString()
	if !known {
		return value.DynamicValue(image.Source(), value.KindUnknown, image.Origin())
	}
	if strings.TrimSpace(text) == "" {
		return value.Value{}
	}
	return value.YAMLScalar("sh", "!!str", image.Origin())
}
