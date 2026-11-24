package execution

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

// ResolveShell applies step, job, workflow and runner shell precedence.
func ResolveShell(step, job, workflow, runner value.Value) scripts.Shell {
	for _, candidate := range []value.Value{step, job, workflow} {
		if candidate.State() == value.Missing {
			continue
		}
		text, ok := executableShellText(candidate)
		if !ok {
			return scripts.Shell{}
		}
		if text != "" {
			return scripts.Shell{Name: text}
		}
	}
	if runner.State() == value.Dynamic || runner.State() == value.Invalid {
		return scripts.Shell{}
	}
	if runner.State() != value.Missing {
		name, ok := runner.ScalarString()
		if !ok {
			return scripts.Shell{}
		}
		return scripts.Shell{Name: name, Implicit: true}
	}
	// Static analysis cannot discover whether a remote runner lacks Bash.
	return scripts.Shell{Name: "bash", Implicit: true}
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
