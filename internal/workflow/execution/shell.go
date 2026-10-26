package execution

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

// Shell distinguishes the runner's bash -e fallback from explicit bash,
// which also enables pipefail. Custom templates supply their own options.
type Shell struct {
	Name     string
	Implicit bool
}

// ResolveShell applies step, job, workflow and runner shell precedence.
func ResolveShell(step, job, workflow, runner value.Value) Shell {
	for _, candidate := range []value.Value{step, job, workflow} {
		if candidate.State() == value.Missing {
			continue
		}
		text, ok := executableShellText(candidate)
		if !ok {
			return Shell{}
		}
		if text != "" {
			return Shell{Name: text}
		}
	}
	if runner.State() == value.Dynamic || runner.State() == value.Invalid {
		return Shell{}
	}
	if runner.State() != value.Missing {
		name, ok := runner.ScalarString()
		if !ok {
			return Shell{}
		}
		return Shell{Name: name, Implicit: true}
	}
	// Static analysis cannot discover whether a remote runner lacks Bash.
	return Shell{Name: "bash", Implicit: true}
}

func executableShellText(candidate value.Value) (string, bool) {
	if text, known := candidate.ScalarString(); known {
		return text, true
	}
	if candidate.State() != value.Dynamic || candidate.Kind() != value.String {
		return "", false
	}
	text := candidate.Source()
	command, _, _ := strings.Cut(text, " ")
	if strings.Contains(command, "${{") {
		return "", false
	}
	return text, true
}

// DefaultShell projects the shell property from a canonical defaults.run value.
func DefaultShell(semantic value.Value) value.Value {
	if semantic.State() == value.Missing {
		return value.Value{}
	}
	if !semantic.Known() {
		return value.DynamicValue(semantic.Source(), value.KindUnknown, semantic.Origin())
	}
	if semantic.Kind() != value.Object {
		return value.DynamicValue(semantic.Source(), value.KindUnknown, semantic.Origin())
	}
	shell, present := semantic.Prop("shell")
	if !present {
		return value.Value{}
	}
	if _, scalar := shell.ScalarString(); !scalar {
		return value.DynamicValue(shell.Source(), value.KindUnknown, shell.Origin())
	}
	return shell
}

// ContainerShell resolves the known job container's default shell.
func ContainerShell(container value.Value) value.Value {
	if container.State() == value.Missing {
		return value.Value{}
	}
	if !container.Known() {
		return value.DynamicValue(container.Source(), value.KindUnknown, container.Origin())
	}
	if container.Kind() != value.Object {
		return value.DynamicValue(container.Source(), value.KindUnknown, container.Origin())
	}
	image, ok := container.Prop("image")
	if !ok {
		return value.DynamicValue("", value.KindUnknown, container.Origin())
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
