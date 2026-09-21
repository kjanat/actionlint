package actionlint

import "strings"

// implicit distinguishes the runner's bash -e fallback from explicit bash,
// which also enables pipefail. Custom templates supply their own options.
type shellcheckShell struct {
	name     string
	implicit bool
}

func (rule *RuleShellcheck) resolveShell(exec *ExecRun) shellcheckShell {
	for _, candidate := range []shellValue{shellValueFromString(exec.Shell), rule.jobShell, rule.workflowShell} {
		candidate = shellcheckLiteral(candidate)
		switch candidate.kind {
		case shellValueSource, shellValueEvaluated:
			if candidate.value.Value != "" {
				return shellcheckShell{name: candidate.value.Value}
			}
		case shellValueUnknown:
			return shellcheckShell{}
		case shellValueUnspecified:
			continue
		}
	}
	if rule.runnerShell.kind == shellValueUnknown {
		return shellcheckShell{}
	}
	if rule.runnerShell.value != nil {
		return shellcheckShell{name: rule.runnerShell.value.Value, implicit: true}
	}
	// Static analysis cannot discover whether a remote runner lacks Bash.
	return shellcheckShell{name: "bash", implicit: true}
}

func shellcheckLiteral(value shellValue) shellValue {
	if value.kind != shellValueSource || !value.value.ContainsExpression() {
		return value
	}
	if literal, known := workflowExpressionLiteral(value.value); known {
		if text, ok := workflowScalarString(literal); ok {
			return shellValue{kind: shellValueEvaluated, value: &String{Value: text}}
		}
	}
	// A custom template can have a known executable and dynamic arguments.
	command, _, _ := strings.Cut(value.value.Value, " ")
	if !strings.Contains(command, "${{") {
		return value
	}
	return shellValue{kind: shellValueUnknown}
}

func shellcheckContainerShell(container *Container) shellValue {
	if container == nil {
		return shellValue{}
	}
	image := shellValueFromString(container.Image)
	if container.Expression != nil {
		value, known := workflowExpressionLiteral(container.Expression)
		if !known {
			return shellValue{kind: shellValueUnknown}
		}
		if object, ok := value.(map[string]any); ok {
			value = workflowObjectProperty(object, "image")
		}
		text, ok := workflowScalarString(value)
		if !ok {
			return shellValue{kind: shellValueUnknown}
		}
		image = shellValue{kind: shellValueEvaluated, value: &String{Value: text}}
	} else if image.kind == shellValueSource && image.value.ContainsExpression() {
		value, known := workflowExpressionLiteral(image.value)
		text, scalar := workflowScalarString(value)
		if !known || !scalar {
			return shellValue{kind: shellValueUnknown}
		}
		image = shellValue{kind: shellValueEvaluated, value: &String{Value: text}}
	}
	if image.value == nil || image.value.Value == "" {
		return shellValue{}
	}
	return shellValueFromString(&String{Value: "sh"})
}

func (shell shellcheckShell) analysis() (dialect, setup string) {
	// Match the runner's first-space split, not a shell command evaluator.
	command, arguments, _ := strings.Cut(shell.name, " ")
	dialect = strings.ToLower(command)
	if dialect != "bash" && dialect != "sh" {
		return "", ""
	}
	if strings.TrimSpace(arguments) == "" {
		if dialect == "bash" && !shell.implicit {
			return dialect, "set -e -o pipefail"
		}
		return dialect, "set -e"
	}
	// The runner passes an argument template directly to the process, not a
	// shell parser. Infer only plain options; quoted/escaped and dynamic templates
	// require platform-specific argument parsing that cannot be assumed here.
	if strings.Contains(arguments, "${{") || strings.ContainsAny(arguments, "'\"\\") {
		return dialect, ""
	}
	args := strings.Fields(arguments)
	var options []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--noprofile" || arg == "--norc":
			continue
		case arg == "-o" || arg == "+o":
			i++
			if i >= len(args) || (args[i] != "pipefail" && args[i] != "errexit" && args[i] != "nounset") {
				return dialect, ""
			}
			options = append(options, arg, args[i])
		case len(arg) > 1 && (arg[0] == '-' || arg[0] == '+') && strings.Trim(arg[1:], "euxv") == "":
			options = append(options, arg)
		case arg == "{0}" || arg == "--":
			if len(options) != 0 {
				return dialect, "set " + strings.Join(options, " ")
			}
			return dialect, ""
		default:
			// Unknown startup flags may consume arguments or change execution mode.
			return dialect, ""
		}
	}
	return dialect, ""
}
