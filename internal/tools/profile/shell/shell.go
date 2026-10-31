// Package shell is the shell preparation profile: it infers a run block's
// dialect and runtime startup options from the step shell, honours a leading
// `# shellcheck shell=` directive, masks templates, inserts directive and
// startup lines and maps the prepared rows back to the run block. Settings
// arrive as typed values; the profile never reads configuration files.
package shell

import (
	"path"
	"strings"
)

// ID names this profile in tool provider manifests.
const ID = "kjanat:actionlint/shell-preparation@0.2.0"

// Shell is the shell command a run block executes under. Implicit marks the
// runner's default shell, which omits pipefail.
type Shell struct {
	Name     string
	Implicit bool
}

// Analysis infers the script dialect and the startup command that reproduces
// the runner's shell options. An empty dialect means the run block is not a
// supported shell script; an empty startup means no options are known.
func (s Shell) Analysis() (dialect, startup string) {
	// Split at the first space to match the runner's executable/argument boundary.
	command, arguments, _ := strings.Cut(s.Name, " ")
	dialect = strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "/"))), ".exe")
	switch dialect {
	case "bash", "sh", "dash", "ksh":
	default:
		return "", ""
	}
	if strings.TrimSpace(arguments) == "" {
		// Only the runner's built-in names receive its default argument templates.
		if !strings.EqualFold(command, "bash") && !strings.EqualFold(command, "sh") {
			return dialect, ""
		}
		if dialect == "bash" && !s.Implicit {
			return dialect, "set -e -o pipefail"
		}
		return dialect, "set -e"
	}
	// Infer plain options only; quoted, escaped and dynamic templates require
	// platform-specific parsing of process arguments.
	args := strings.Fields(arguments)
	for _, arg := range args {
		arg = strings.Trim(arg, "'\"")
		if arg == "{0}" || arg == "--" {
			break
		}
		// With -c the file may only be $0; with -s it is an argument to stdin.
		// Neither establishes the language of the run block.
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg[1:], "cs") {
			return "", ""
		}
	}
	if strings.Contains(arguments, "${{") || strings.ContainsAny(arguments, "'\"\\") {
		return dialect, ""
	}
	var options []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "{0}" || arg == "--":
			if arg == "--" && (i+1 >= len(args) || args[i+1] != "{0}") {
				return "", ""
			}
			return dialect, startupOptions(options)
		case dialect == "bash" && (arg == "--noprofile" || arg == "--norc"):
			continue
		case len(arg) > 1 && (arg[0] == '-' || arg[0] == '+'):
			flags := arg[1:]
			if before, ok := strings.CutSuffix(flags, "o"); ok {
				flags = before
				i++
				if i >= len(args) || !setOption(args[i]) {
					return dialect, ""
				}
				if strings.Trim(flags, "abefhkmnptuvxBCEHPT") != "" {
					return dialect, ""
				}
				if flags != "" {
					options = append(options, arg[:1]+flags)
				}
				options = append(options, arg[:1]+"o", args[i])
			} else {
				if strings.Trim(flags, "abefhkmnptuvxBCEHPT") != "" {
					return dialect, ""
				}
				options = append(options, arg)
			}
		default:
			// A different script filename before {0} makes the run block an argument.
			return "", ""
		}
	}
	return dialect, ""
}

// ShellCheck's hasSetE/hasPipefail checks ignore later +e/+o overrides,
// so startup options must describe the final enabled state.
func startupOptions(options []string) string {
	aliases := map[string]string{
		"allexport": "a", "errexit": "e", "errtrace": "E", "functrace": "T",
		"noclobber": "C", "noexec": "n", "noglob": "f", "nounset": "u",
		"verbose": "v", "xtrace": "x",
	}
	states := make(map[string]bool)
	var order []string
	set := func(option string, enabled bool) {
		if alias, ok := aliases[option]; ok {
			option = alias
		}
		if _, exists := states[option]; !exists {
			order = append(order, option)
		}
		states[option] = enabled
	}
	for i := 0; i < len(options); i++ {
		option := options[i]
		enabled := option[0] == '-'
		if option[1:] == "o" {
			i++
			set(options[i], enabled)
		} else {
			for _, flag := range option[1:] {
				set(string(flag), enabled)
			}
		}
	}
	var flags strings.Builder
	var named []string
	for _, option := range order {
		if !states[option] {
			continue
		}
		if len(option) == 1 {
			flags.WriteString(option)
		} else {
			named = append(named, "-o", option)
		}
	}
	var args []string
	if flags.Len() > 0 {
		args = append(args, "-"+flags.String())
	}
	args = append(args, named...)
	if len(args) == 0 {
		return ""
	}
	return "set " + strings.Join(args, " ")
}

func setOption(option string) bool {
	switch option {
	case "allexport", "errexit", "errtrace", "functrace", "noclobber", "noexec", "noglob", "nounset", "pipefail", "posix", "verbose", "xtrace":
		return true
	default:
		return false
	}
}

// LanguageDialect is the dialect of an action input declared in language;
// empty when the language is not a supported shell.
func LanguageDialect(language string) string {
	switch language {
	case "bash", "sh", "dash", "ksh", "busybox":
		return language
	default:
		return ""
	}
}
