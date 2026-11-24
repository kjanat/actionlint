package scripts

import (
	"path"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
)

// ShellExecutableName returns env for a wrapper such as "env bash".
func ShellExecutableName(shell string) string {
	command, _, _ := strings.Cut(shell, " ")
	return strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "/"))), ".exe")
}

// DefaultShell returns an unknown value with no Bash default for an unknown
// container or empty runner shell.
func DefaultShell(container value.Value, runnerShell string) value.Value {
	if shell := ContainerShell(container); shell.State() != value.Missing {
		return shell
	}
	if runnerShell == "" {
		return value.DynamicValue("", value.KindUnknown, value.Origin{})
	}
	return value.YAMLScalar(runnerShell, "!!str", value.Origin{})
}

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
