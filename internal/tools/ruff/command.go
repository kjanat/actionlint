package ruff

import (
	"fmt"
	"strings"

	pythonprofile "actionlint.kjanat.dev/v2/internal/tools/profile/python"
)

type commandPolicy struct {
	flags            []string
	commandError     error
	workingDirectory string
}

func ruffOptionTakesValue(option string) bool {
	switch option {
	case "--select", "--ignore", "--extend-select", "--extend-ignore", "--per-file-ignores", "--extend-per-file-ignores",
		"--fixable", "--unfixable", "--extend-fixable", "--extend-unfixable", "--exclude", "--extend-exclude",
		"--cache-dir", "--config", "--color", "--line-length":
		return true
	default:
		return false
	}
}

// CommandArguments preserves launcher operands and validates checker arguments
// when an eligible Python script is checked.
func (c *commandPolicy) CommandArguments(executable string, args []string) []string {
	prefix, flags, err := commandArguments(executable, args)
	c.flags, c.commandError = flags, err
	return prefix
}

func commandArguments(executable string, args []string) (prefix, flags []string, err error) {
	name := commandBase(executable)
	if pythonprofile.IsCommand(executable) {
		return pythonModuleArguments(name, args)
	}
	if name != "env" && name != "uvx" {
		return nil, args, nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			i++
			if i < len(args) && ruffLauncherOperand(name, args[i]) {
				return args[:i+1], args[i+1:], nil
			}
			break
		}
		option, _, attached := strings.Cut(arg, "=")
		if name == "env" && !strings.HasPrefix(arg, "-") && attached && option != "" {
			if strings.EqualFold(option, "RUFF_OUTPUT_FILE") {
				break
			}
			continue
		}
		if ruffLauncherOperand(name, arg) {
			return args[:i+1], args[i+1:], nil
		}
		if name == "env" {
			if arg == "-i" || arg == "--ignore-environment" {
				continue
			}
			if option != "-u" && option != "--unset" {
				break
			}
		} else {
			if arg == "--offline" || arg == "--no-cache" {
				continue
			}
			if option != "--from" {
				break
			}
		}
		if !attached {
			i++
			if i == len(args) {
				break
			}
		}
	}
	return nil, nil, fmt.Errorf("unsupported Ruff launcher arguments for %q: %q; specify a Ruff command after supported launcher options", name, args)
}

// Python consumes interpreter options before -m, but everything after the
// module name belongs to Ruff. Keep that boundary ahead of check and --version.
func pythonModuleArguments(name string, args []string) (prefix, flags []string, err error) {
	selectorSeen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-m" && i+1 < len(args) && args[i+1] == "ruff" {
			return args[:i+2], args[i+2:], nil
		}
		if arg == "-mruff" {
			return args[:i+1], args[i+1:], nil
		}
		if name == "py" && !selectorSeen && pythonLauncherSelector(arg) {
			selectorSeen = true
			continue
		}
		if arg == "--check-hash-based-pycs" {
			i++
			if i < len(args) && (args[i] == "default" || args[i] == "always" || args[i] == "never") {
				continue
			}
			break
		}
		if !strings.HasPrefix(arg, "-") || len(arg) < 2 || arg[1] == '-' {
			break
		}
		valid := true
		for j := 1; j < len(arg); j++ {
			switch arg[j] {
			case 'm':
				if arg[j+1:] == "ruff" {
					return args[:i+1], args[i+1:], nil
				}
				if j+1 == len(arg) && i+1 < len(args) && args[i+1] == "ruff" {
					return args[:i+2], args[i+2:], nil
				}
				valid = false
			case 'b', 'B', 'd', 'E', 'I', 'O', 'P', 'q', 'R', 's', 'S', 'u', 'v', 'x':
				// CPython accepts combined short options, e.g. -IuB.
			case 'W', 'X':
				// The value may be attached or the next argv operand.
				if j+1 == len(arg) {
					i++
					valid = i < len(args) && args[i] != ""
				}
				j = len(arg)
			default:
				valid = false
			}
			if !valid {
				break
			}
		}
		if !valid {
			break
		}
	}
	return nil, nil, fmt.Errorf("unsupported Ruff Python launcher arguments for %q: %q; use supported interpreter options followed by -m ruff, not a script, another module, or -c", name, args)
}

func pythonLauncherSelector(arg string) bool {
	version, ok := strings.CutPrefix(arg, "-V:")
	if ok {
		if company, tag, qualified := strings.Cut(version, "/"); qualified {
			if !strings.EqualFold(company, "PythonCore") {
				return false
			}
			version = tag
		}
	} else {
		version, ok = strings.CutPrefix(arg, "-")
		if !ok {
			return false
		}
	}
	version, architecture, qualified := strings.Cut(version, "-")
	if qualified && architecture != "32" && architecture != "64" && architecture != "arm64" {
		return false
	}
	if version == "3" {
		return true
	}
	minor, ok := strings.CutPrefix(version, "3.")
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

func ruffLauncherOperand(launcher, arg string) bool {
	if strings.HasPrefix(arg, "-") {
		return false
	}
	if commandBase(arg) == "ruff" {
		return true
	}
	version, pinned := strings.CutPrefix(arg, "ruff@")
	return launcher == "uvx" && pinned && version != "" && !strings.ContainsAny(version, " /\\\t\r\n<>=,")
}

func commandBase(command string) string {
	return strings.TrimSuffix(strings.ToLower(command[strings.LastIndexAny(command, `/\`)+1:]), ".exe")
}

func (c *commandPolicy) validateCommand(location string) error {
	if c.commandError != nil {
		return c.commandError
	}
	for _, flag := range c.flags {
		if strings.HasPrefix(flag, "@") {
			return fmt.Errorf("ruff argument files are not supported for script at %s: %q", location, flag)
		}
	}
	for i := 0; i < len(c.flags); i++ {
		flag := c.flags[i]
		if !strings.HasPrefix(flag, "-") || flag == "-" || flag == "--" {
			return fmt.Errorf("ruff extra file operands are not supported for script at %s: %q", location, flag)
		}
		if flag == "--show-files" || flag == "--show-settings" {
			return fmt.Errorf("ruff inspection output is not supported for script at %s: %q", location, flag)
		}
		if flag == "--diff" || flag == "--fix-only" {
			return fmt.Errorf("ruff editing output is not supported for script at %s: %q", location, flag)
		}
		if flag == "--statistics" {
			return fmt.Errorf("ruff statistics output is not supported for script at %s", location)
		}
		if flag == "--output-file" || strings.HasPrefix(flag, "--output-file=") ||
			strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") && strings.ContainsRune(flag[1:], 'o') {
			return fmt.Errorf("ruff output redirection is not supported for script at %s: %q", location, flag)
		}
		if flag == "--silent" || strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") && strings.ContainsRune(flag[1:], 's') {
			return fmt.Errorf("ruff silent output is not supported for script at %s", location)
		}
		option, _, _ := strings.Cut(flag, "=")
		if option == "--extension" {
			return fmt.Errorf("ruff source-type overrides are not supported for script at %s", location)
		}
		if option == "--help" || option == "--watch" || option == "--add-noqa" || option == "--add-ignore" ||
			strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") && strings.ContainsAny(option[1:], "hw") {
			return fmt.Errorf("ruff non-diagnostic mode %q is not supported for script at %s", option, location)
		}
		if strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") && strings.ContainsRune(option[1:], 'n') {
			option = "--no-cache"
		}
		switch option {
		case "--isolated", "--ignore-noqa", "--no-fix", "--no-cache", "--target-version", "--stdin-filename", "--output-format":
			return fmt.Errorf("ruff integration-owned option %q must be removed from extra arguments for script at %s", option, location)
		}
		if option == "--config" {
			_, value, attached := strings.Cut(flag, "=")
			if !attached && i+1 < len(c.flags) {
				value = c.flags[i+1]
			}
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("ruff option %q requires a value for script at %s", option, location)
			}
			if value != "" && !strings.HasPrefix(value, "-") && !ruffInlineConfig(value, c.workingDirectory) {
				return fmt.Errorf("ruff configuration files cannot be used in isolated mode for script at %s; use inline --config KEY=VALUE overrides", location)
			}
		}
		if ruffOptionTakesValue(option) && !strings.Contains(flag, "=") {
			if i+1 == len(c.flags) || strings.HasPrefix(c.flags[i+1], "-") {
				return fmt.Errorf("ruff option %q requires a value for script at %s", option, location)
			}
			i++
		}
	}
	return nil
}
