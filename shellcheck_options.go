package actionlint

import (
	"os"
	"runtime"
	"strings"
)

// Read only the selector; ShellCheck remains responsible for validating flags.
// Options consuming a value must be skipped before looking for another selector.
func shellcheckDialectOption(args []string) (string, bool) {
	var dialect string
	var found bool
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "--") {
			name, value, attached := strings.Cut(arg[2:], "=")
			switch name {
			case "shell", "include", "exclude", "extended-analysis", "format", "rcfile", "enable", "source-path", "severity", "wiki-link-count":
				if !attached && i+1 < len(args) {
					i++
					value = args[i]
				}
				if name == "shell" {
					dialect, found = value, true
				}
			}
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		for j := 1; j < len(arg); j++ {
			option := arg[j]
			if option == 'C' { // Optional color value must be attached.
				break
			}
			if strings.ContainsRune("iefosPSW", rune(option)) {
				value := arg[j+1:]
				if value == "" && i+1 < len(args) {
					i++
					value = args[i]
				}
				if option == 's' {
					dialect, found = value, true
				}
				break
			}
		}
	}
	return dialect, found
}

func (cmd *externalCommand) shellcheckDialect() (string, bool) {
	value := os.Getenv("SHELLCHECK_OPTS")
	for _, entry := range cmd.env {
		key, override, _ := strings.Cut(entry, "=")
		if key == "SHELLCHECK_OPTS" || runtime.GOOS == "windows" && strings.EqualFold(key, "SHELLCHECK_OPTS") {
			value = override
		}
	}
	// ShellCheck splits this variable on spaces, without shell quoting or expansion.
	var args []string
	for arg := range strings.SplitSeq(value, " ") {
		if arg != "" {
			args = append(args, arg)
		}
	}
	args = append(args, cmd.args...)
	return shellcheckDialectOption(args)
}
