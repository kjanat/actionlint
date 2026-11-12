package correctness

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/interpreters"
	"actionlint.kjanat.dev/v2/internal/model/commands"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

// checkShell validates builtin names and accepts custom shell templates.
func checkShell(shell value.Value, platform runnerlang.Platform, registries ...*commands.Registry) []results.Diagnostic {
	node, known := shell.ScalarString()
	if !known {
		return nil
	}

	registry := interpreters.Builtin()
	if len(registries) > 0 && registries[0] != nil {
		registry = registries[0]
	}
	arguments := commands.UnknownArguments
	if platform == runnerlang.MacOrLinux {
		arguments = commands.UnixArguments
	}
	if platform == runnerlang.Windows {
		arguments = commands.WindowsArguments
	}
	invocation := registry.AnalyzeFor(node, false, arguments)
	if len(invocation.Issues) != 0 {
		var findings []results.Diagnostic
		for _, issue := range invocation.Issues {
			findings = append(findings, results.Diagnostic{Rule: "shell-name", Code: issue.Code, Severity: issue.Severity, Start: shell.Origin().Span.Start, End: shell.Origin().Span.End, Message: issue.Message})
		}
		return findings
	}
	// Custom interpreters can supply their own definition. An unregistered
	// executable supplies no language or option-validity conclusion.
	if strings.Contains(node, "{0}") {
		return nil
	}

	name := strings.ToLower(node)
	available := getAvailableShellNames(platform)

	if slices.Contains(available, name) {
		return nil // ok
	}

	onPlatform := ""
	switch platform {
	case runnerlang.Windows:
		for _, p := range getAvailableShellNames(runnerlang.Any) {
			if name == p {
				onPlatform = " on Windows" // only when the shell is unavailable on Windows
			}
		}
	case runnerlang.MacOrLinux:
		for _, p := range getAvailableShellNames(runnerlang.Any) {
			if name == p {
				onPlatform = " on macOS or Linux" // only when the shell is unavailable on macOS or Linux
			}
		}
	case runnerlang.Any:
	}

	position := shell.Origin().Span.Start
	return []results.Diagnostic{{Rule: "shell-name", Start: position, Message: fmt.Sprintf("shell name %q is invalid%s. available names are %s", node, onPlatform, messages.SortedQuotes(available))}}
}

func getAvailableShellNames(kind runnerlang.Platform) []string {
	// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#using-a-specific-shell
	switch kind {
	case runnerlang.Any:
		return []string{
			"bash",
			"pwsh",
			"python",
			"sh",
			"cmd",
			"powershell",
		}
	case runnerlang.Windows:
		return []string{
			"bash",
			"pwsh",
			"python",
			"cmd",
			"powershell",
		}
	case runnerlang.MacOrLinux:
		return []string{
			"bash",
			"pwsh",
			"python",
			"sh",
		}
	default:
		panic("unreachable")
	}
}
