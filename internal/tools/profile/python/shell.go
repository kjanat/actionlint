package python

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/mattn/go-shellwords"
)

// IsShell reports whether a step shell runs its script with Python, directly
// or through the documented actions-shell Python adapters.
func IsShell(shell string) bool {
	command, args := pythonShellCommand(shell)
	if command == "" {
		return false
	}
	if IsCommand(command) {
		return true
	}
	return pythonAdapterArguments(args, command) != nil
}

// Parse the executable separately to preserve Windows path backslashes.
func pythonShellCommand(shell string) (command, args string) {
	shell = strings.TrimSpace(shell)
	if shell == "" {
		return "", ""
	}
	if quote := shell[0]; quote == '"' || quote == '\'' {
		end := strings.IndexByte(shell[1:], quote)
		if end < 0 {
			return "", ""
		}
		end++
		if end+1 < len(shell) && !unicode.IsSpace(rune(shell[end+1])) {
			return "", ""
		}
		return shell[1:end], shell[end+1:]
	}
	end := strings.IndexFunc(shell, unicode.IsSpace)
	if end < 0 {
		return shell, ""
	}
	return shell[:end], shell[end:]
}

func pythonAdapterArguments(arguments, command string) []string {
	name := strings.ToLower(command[strings.LastIndexAny(command, `/\`)+1:])
	name = strings.TrimSuffix(name, ".cmd")
	if name != "actions-shell" && name != "actions-shells" {
		return nil
	}
	// The documented Python adapters take the runtime first and script last.
	parser := shellwords.NewParser()
	parser.ParseEnv, parser.ParseBacktick = false, false
	args, err := parser.Parse(arguments)
	if err != nil || parser.Position >= 0 || len(args) < 2 ||
		(args[0] != "python" && args[0] != "py") || args[len(args)-1] != "{0}" {
		return nil
	}
	return args
}

// IsCommand reports whether an executable path names a Python 3 interpreter
// or the py launcher.
func IsCommand(command string) bool {
	command = command[strings.LastIndexAny(command, `/\`)+1:]
	command = strings.TrimSuffix(strings.ToLower(command), ".exe")
	if command == "python" || command == "python3" || command == "py" {
		return true
	}
	minor, ok := strings.CutPrefix(command, "python3.")
	if !ok || minor == "" {
		return false
	}
	for _, digit := range minor {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

// ShellTarget is the Python target version, such as py312, a versioned
// interpreter or py launcher selector in shell names; empty when the shell
// leaves the version open.
func ShellTarget(shell string) string {
	executable, arguments := pythonShellCommand(shell)
	if executable == "" {
		return ""
	}
	words := append([]string{executable}, strings.Fields(arguments)...)
	if args := pythonAdapterArguments(arguments, executable); args != nil {
		words = args
	}
	command := words[0]
	command = command[strings.LastIndexAny(command, `/\`)+1:]
	command = strings.TrimSuffix(strings.ToLower(command), ".exe")
	if command == "py" && len(words) >= 3 && slices.ContainsFunc(words[2:], func(word string) bool {
		return word == "{0}" || word == `"{0}"` || word == "'{0}'"
	}) {
		selector := words[1]
		if len(selector) >= 2 && (selector[0] == '"' || selector[0] == '\'') && selector[len(selector)-1] == selector[0] {
			selector = selector[1 : len(selector)-1]
		}
		version, ok := strings.CutPrefix(selector, "-V:")
		if !ok {
			version, ok = strings.CutPrefix(selector, "-")
		}
		if !ok {
			return ""
		}
		if company, tag, hasCompany := strings.Cut(version, "/"); hasCompany {
			if !strings.HasPrefix(selector, "-V:") || !strings.EqualFold(company, "PythonCore") {
				return ""
			}
			version = tag
		}
		version, architecture, hasArchitecture := strings.Cut(version, "-")
		if hasArchitecture && architecture != "32" && architecture != "64" && architecture != "arm64" {
			return ""
		}
		command = "python" + version
	}
	if minor, ok := strings.CutPrefix(command, "python3."); ok {
		if version, err := strconv.Atoi(minor); err == nil && version >= 0 && strconv.Itoa(version) == minor {
			return "py3" + minor
		}
	}
	return ""
}
