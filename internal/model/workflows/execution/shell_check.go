package execution

import (
	"fmt"
	"slices"
	"strings"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

// CheckShell validates builtin names and accepts custom shell templates.
func CheckShell(shell value.Value, platform runnerlang.Platform) []results.Diagnostic {
	node, known := shell.ScalarString()
	if !known {
		return nil
	}

	// Ignore custom shell
	// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#custom-shell
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
