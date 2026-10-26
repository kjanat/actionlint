package execution

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/stringutil"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

// CheckShell validates builtin names and accepts custom shell templates.
func CheckShell(shell value.Value, platform runnerlang.Platform) []diagnostic.Diagnostic {
	if !shell.Known() || shell.Kind() != value.String {
		return nil
	}
	node, _ := shell.Text()

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
	}

	position := shell.Origin().Span.Start
	return []diagnostic.Diagnostic{{Rule: "shell-name", Start: position, Message: fmt.Sprintf("shell name %q is invalid%s. available names are %s", node, onPlatform, stringutil.SortedQuotes(available))}}
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
