package ruff

import (
	"fmt"
	"strings"
)

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
func (c *Checker) CommandArguments(executable string, args []string) []string {
	prefix, flags, err := commandArguments(executable, args)
	c.flags, c.commandError = flags, err
	return prefix
}

func commandArguments(executable string, args []string) (prefix, flags []string, err error) {
	name := commandBase(executable)
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
