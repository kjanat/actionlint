package commands

import (
	"slices"
	"strings"
)

func BashDefinition() Definition {
	definition := Definition{
		ID: "bash", Executables: []string{"bash", "bash.exe"}, IgnoreExecutableCase: true,
		Language: "bash", ToggleOptions: true, LongOptionsFirst: true,
		DefaultArguments: []string{"-e", "-o", "pipefail", "{0}"}, ImplicitArguments: []string{"-e", "{0}"},
	}
	flags := map[string]string{
		"a": "allexport", "b": "notify", "B": "braceexpand", "C": "noclobber", "e": "errexit", "E": "errtrace",
		"f": "noglob", "h": "hashall", "H": "histexpand", "k": "keyword", "m": "monitor", "n": "noexec",
		"p": "privileged", "t": "onecmd", "T": "functrace", "u": "nounset", "v": "verbose", "x": "xtrace", "P": "physical",
	}
	for flag, name := range flags {
		definition.Options = append(definition.Options, Option{Short: flag, Group: "set", Effect: name})
	}
	for _, flag := range []string{"i", "l", "r", "D"} {
		definition.Options = append(definition.Options, Option{Short: flag})
	}
	definition.Options = append(definition.Options,
		Option{Short: "c", Input: "command"}, Option{Short: "s", Input: "stdin"},
		Option{Short: "o", Argument: true, Group: "set", Choices: strings.Fields("allexport braceexpand emacs errexit errtrace functrace hashall histexpand history igncr ignoreeof interactive-comments keyword monitor noclobber noexec noglob nolog notify nounset onecmd physical pipefail posix privileged verbose vi xtrace")},
		Option{Short: "O", Argument: true, Group: "shopt", Choices: strings.Fields("array_expand_once assoc_expand_once autocd bash_source_fullpath cdable_vars cdspell checkhash checkjobs checkwinsize cmdhist compat31 compat32 compat40 compat41 compat42 compat43 compat44 completion_strip_exe complete_fullquote direxpand dirspell dotglob execfail expand_aliases extdebug extglob extquote failglob force_fignore globasciiranges globskipdots globstar gnu_errfmt histappend histreedit histverify hostcomplete huponexit inherit_errexit interactive_comments lastpipe lithist localvar_inherit localvar_unset login_shell mailwarn no_empty_cmd_completion nocaseglob nocasematch noexpand_translation nullglob patsub_replacement progcomp progcomp_alias promptvars restricted_shell shift_verbose sourcepath varredir_close xpg_echo")},
	)
	for _, name := range []string{"debug", "protected", "noprofile", "norc", "login", "restricted", "verbose", "debugger", "noediting", "nolineediting", "wordexp", "pretty-print", "dump-strings", "dump-po-strings"} {
		definition.Options = append(definition.Options, Option{Long: name})
	}
	definition.Options = append(definition.Options, Option{Long: "help", ExitAfter: "long-options"}, Option{Long: "version", ExitAfter: "long-options"}, Option{Long: "posix", Group: "set", Effect: "posix"}, Option{Long: "rcfile", Argument: true}, Option{Long: "init-file", Argument: true})
	slices.SortFunc(definition.Options, func(a, b Option) int { return strings.Compare(a.Short+a.Long, b.Short+b.Long) })
	return definition
}

// BashSetup describes the script's initial options and omits earlier flags.
// Changes with side effects preserve their order. POSIX mode changes shopt
// settings, and selecting an editing mode disables the other editing mode.
func BashSetup(settings []OptionSetting) string {
	settings = initialSettings(settings)
	aliases := map[string]string{"allexport": "a", "notify": "b", "braceexpand": "B", "noclobber": "C", "errexit": "e", "errtrace": "E", "noglob": "f", "hashall": "h", "histexpand": "H", "keyword": "k", "monitor": "m", "noexec": "n", "privileged": "p", "onecmd": "t", "functrace": "T", "nounset": "u", "verbose": "v", "xtrace": "x", "physical": "P"}
	var lines []string
	for i := 0; i < len(settings); {
		setting := settings[i]
		if setting.Group == "shopt" {
			flag := "-u"
			if setting.Enabled {
				flag = "-s"
			}
			lines = append(lines, "shopt "+flag+" "+quote(setting.Name))
			i++
			continue
		}
		if setting.Group != "set" {
			i++
			continue
		}
		sign := "+"
		if setting.Enabled {
			sign = "-"
		}
		var flags strings.Builder
		var named []string
		for i < len(settings) && settings[i].Group == "set" && settings[i].Enabled == setting.Enabled {
			option := settings[i]
			if alias, exists := aliases[option.Name]; exists && len(named) == 0 {
				flags.WriteString(alias)
			} else {
				named = append(named, sign+"o", quote(option.Name))
			}
			i++
		}
		var args []string
		if flags.Len() != 0 {
			args = append(args, sign+flags.String())
		}
		args = append(args, named...)
		lines = append(lines, "set "+strings.Join(args, " "))
	}
	return strings.Join(lines, "; ")
}

func initialSettings(settings []OptionSetting) []OptionSetting {
	last := make(map[string]int)
	editing := -1
	independent := func(setting OptionSetting) bool {
		if setting.Group != "set" {
			return false
		}
		switch setting.Name {
		case "posix", "emacs", "vi", "privileged":
			return false
		default:
			return true
		}
	}
	// These options start disabled in a noninteractive Bash invocation. Trace
	// inheritance is excluded. extdebug changes trace inheritance. POSIX and editing
	// modes have separate transitions and no independent Boolean flags.
	disabledByDefault := strings.Fields("allexport errexit history ignoreeof keyword monitor noclobber noexec noglob nolog notify nounset onecmd physical pipefail verbose xtrace")
	for i, setting := range settings {
		if independent(setting) {
			last[setting.Name] = i
		}
		if setting.Group == "set" && (setting.Name == "emacs" || setting.Name == "vi") {
			editing = i
		}
	}
	result := make([]OptionSetting, 0, len(settings))
	for i, setting := range settings {
		if setting.Group == "set" && (setting.Name == "emacs" || setting.Name == "vi") {
			// Bash's invocation parser stores one editing mode. +o emacs and +o vi
			// both select no editing here. The runtime set-builtin behavior of +o emacs and +o vi differs.
			if i == editing && setting.Enabled {
				result = append(result, setting)
			}
			continue
		}
		if independent(setting) {
			if last[setting.Name] != i {
				continue
			}
			if !setting.Enabled && slices.Contains(disabledByDefault, setting.Name) {
				continue
			}
		}
		result = append(result, setting)
	}
	return result
}

func quote(value string) string {
	if value != "" && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == "" {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
