package commands

import (
	"fmt"
	"strings"
)

// WorkflowCommandState follows stdout command processing in actions/runner
// 1ba885c2065fd9be317808ab3fd406f392635cf7, ActionCommand.TryParseV2 and
// ActionCommandManager.TryProcessCommand. Unknown output must end the caller's
// analysis: it could contain a stop command with an unknown resume token.
type WorkflowCommandState struct{ stopToken string }

// WorkflowCommandProblem describes text resembling a runner command that the
// runner prints verbatim. Valid commands can still be rejected by an extension;
// extension-specific properties and server feature flags are not checked here.
func (s *WorkflowCommandState) WorkflowCommandProblem(line string) string {
	line = strings.TrimLeft(line, " \t\r\v\f")
	if !strings.HasPrefix(line, "::") {
		return ""
	}
	info, data, closed := strings.Cut(line[2:], "::")
	name, _, _ := strings.Cut(info, " ")
	name = strings.ToLower(name)
	if s.stopToken != "" {
		if closed && strings.EqualFold(name, s.stopToken) {
			s.stopToken = ""
		}
		return ""
	}
	if closed && knownWorkflowCommand(name) {
		if name == "stop-commands" {
			s.stopToken = unescapeCommandData(data)
		}
		return ""
	}
	// Restrict candidates to command-shaped names. This excludes namespace/type
	// syntax and dynamic output, while retaining misspelled command names.
	if !closed {
		end := strings.IndexAny(name, ":\t\r\n")
		if end >= 0 {
			name = name[:end]
		}
	}
	if name == "" {
		return ""
	}
	for _, c := range name {
		if c < 'a' || c > 'z' {
			if c != '-' {
				return ""
			}
		}
	}
	if !knownWorkflowCommand(name) {
		return fmt.Sprintf("The runner does not recognize workflow command %q and prints it as text.", name)
	}
	return fmt.Sprintf("Workflow command %q has no closing :: separator, so the runner prints it as text.", name)
}

func knownWorkflowCommand(name string) bool {
	switch name {
	case "add-mask", "add-matcher", "add-path", "debug", "echo", "endgroup", "error", "group", "notice", "remove-matcher", "save-state", "set-env", "set-output", "stop-commands", "warning", "internal-set-repo-path":
		return true
	}
	return false
}

func unescapeCommandData(data string) string {
	return strings.NewReplacer("%0D", "\r", "%0A", "\n", "%25", "%").Replace(data)
}
