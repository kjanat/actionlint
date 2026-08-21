package actionlint

import (
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type completionShell string

const (
	completionShellBash       completionShell = "bash"
	completionShellFish       completionShell = "fish"
	completionShellPowerShell completionShell = "powershell"
	completionShellZsh        completionShell = "zsh"
)

var completionShells = []completionShell{
	completionShellBash,
	completionShellFish,
	completionShellPowerShell,
	completionShellZsh,
}

func completionShellNameList() []string {
	names := make([]string, 0, len(completionShells))
	for _, s := range completionShells {
		names = append(names, string(s))
	}
	return names
}

func completionShellNames() string {
	return strings.Join(completionShellNameList(), ", ")
}

func (s *completionShell) String() string {
	return string(*s)
}

func (s *completionShell) Set(v string) error {
	for _, c := range completionShells {
		if completionShell(v) == c {
			*s = c
			return nil
		}
	}
	return fmt.Errorf("must be one of %s", completionShellNames())
}

// repeatableFlag is implemented by a flag.Value which accumulates every occurrence instead of
// overwriting the previous one.
type repeatableFlag interface {
	repeatableFlag()
}

type completionArg int

const (
	completionArgNone completionArg = iota
	completionArgYAMLFile
	completionArgAnyFile
	completionArgCommand
	completionArgShellName
	completionArgOpaque
)

var completionArgOrder = []completionArg{
	completionArgYAMLFile,
	completionArgAnyFile,
	completionArgCommand,
	completionArgShellName,
	completionArgOpaque,
}

var completionArgs = map[string]completionArg{
	"color":          completionArgNone,
	"completion":     completionArgShellName,
	"config-file":    completionArgYAMLFile,
	"debug":          completionArgNone,
	"format":         completionArgOpaque,
	"ignore":         completionArgOpaque,
	"init-config":    completionArgNone,
	"no-color":       completionArgNone,
	"oneline":        completionArgNone,
	"pyflakes":       completionArgCommand,
	"shellcheck":     completionArgCommand,
	"stdin-filename": completionArgAnyFile,
	"verbose":        completionArgNone,
	"version":        completionArgNone,
}

func completionArgOf(f *flag.Flag) completionArg {
	if a, ok := completionArgs[f.Name]; ok {
		return a
	}
	if typeName, _ := flag.UnquoteUsage(f); typeName == "" {
		return completionArgNone
	}
	return completionArgOpaque
}

func completionDescription(f *flag.Flag) string {
	_, usage := flag.UnquoteUsage(f)
	usage, _, _ = strings.Cut(strings.Join(strings.Fields(usage), " "), ". ")
	return usage
}

type completionFlag struct {
	name       string
	desc       string
	arg        completionArg
	repeatable bool
}

func completionFlagsOf(flags *flag.FlagSet) []*completionFlag {
	var fs []*completionFlag
	flags.VisitAll(func(f *flag.Flag) {
		_, repeatable := f.Value.(repeatableFlag)
		fs = append(fs, &completionFlag{
			name:       f.Name,
			desc:       completionDescription(f),
			arg:        completionArgOf(f),
			repeatable: repeatable,
		})
	})
	return fs
}

func completionFlagsWithArg(fs []*completionFlag, a completionArg) []*completionFlag {
	var ret []*completionFlag
	for _, f := range fs {
		if f.arg == a {
			ret = append(ret, f)
		}
	}
	return ret
}

var completionFishSafe = regexp.MustCompile(`\A[A-Za-z0-9_./-]+\z`)

func completionQuoteBash(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func completionQuoteZsh(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`, ":", `\:`)
	return strings.ReplaceAll(r.Replace(s), "'", `'\''`)
}

func completionQuoteFish(s string) string {
	if completionFishSafe.MatchString(s) {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, "'", `\'`)
	return "'" + r.Replace(s) + "'"
}

func completionQuotePwsh(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// writeCompletion writes a shell completion script for the given shell to out. The script completes
// the flags declared in flags, the values they take, and workflow file paths.
func writeCompletion(out io.Writer, shell completionShell, flags *flag.FlagSet) error {
	fs := completionFlagsOf(flags)

	var lines []string
	switch shell {
	case completionShellBash:
		lines = completionBash(fs)
	case completionShellFish:
		lines = completionFish(fs)
	case completionShellPowerShell:
		lines = completionPwsh(fs)
	case completionShellZsh:
		lines = completionZsh(fs)
	default:
		return fmt.Errorf("cannot generate a completion script for shell %q. It must be one of %s", string(shell), completionShellNames())
	}

	_, err := io.WriteString(out, strings.Join(lines, "\n")+"\n")
	return err
}

const completionBashYAMLFiles = `COMPREPLY=( $(compgen -d -- "$cur") $(compgen -f -X '!*.yaml' -- "$cur") $(compgen -f -X '!*.yml' -- "$cur") )`

func completionBashReply(a completionArg) string {
	switch a {
	case completionArgYAMLFile:
		return completionBashYAMLFiles
	case completionArgAnyFile:
		return `COMPREPLY=( $(compgen -f -- "$cur") )`
	case completionArgCommand:
		return `COMPREPLY=( $(compgen -c -- "$cur") )`
	case completionArgShellName:
		return `COMPREPLY=( $(compgen -W "$__shells" -- "$cur") )`
	default:
		return "COMPREPLY=()"
	}
}

func completionBash(fs []*completionFlag) []string {
	names := make([]string, 0, len(fs))
	for _, f := range fs {
		names = append(names, "-"+f.name)
	}

	lines := []string{
		"# bash completion for actionlint",
		"_actionlint() {",
		"    local cur prev IFS",
		"    local __flags=" + completionQuoteBash(strings.Join(names, "\n")),
		"    local __shells=" + completionQuoteBash(strings.Join(completionShellNameList(), "\n")),
		`    cur="${COMP_WORDS[COMP_CWORD]}"`,
		`    prev="${COMP_WORDS[COMP_CWORD-1]}"`,
		`    IFS=$'\n'`,
		"",
		`    case "$prev" in`,
	}

	for _, a := range completionArgOrder {
		matched := completionFlagsWithArg(fs, a)
		if len(matched) == 0 {
			continue
		}
		pats := make([]string, 0, len(matched)*2)
		for _, f := range matched {
			pats = append(pats, completionQuoteBash("-"+f.name), completionQuoteBash("--"+f.name))
		}
		lines = append(
			lines,
			"        "+strings.Join(pats, "|")+")",
			"            "+completionBashReply(a),
			"            return 0",
			"            ;;",
		)
	}

	return append(
		lines,
		"    esac",
		"",
		`    if [[ "$cur" == -* ]]; then`,
		`        COMPREPLY=( $(compgen -W "$__flags" -- "$cur") )`,
		"        return 0",
		"    fi",
		"",
		"    "+completionBashYAMLFiles,
		"    return 0",
		"}",
		"complete -o filenames -F _actionlint actionlint",
	)
}

func completionZshAction(a completionArg) string {
	switch a {
	case completionArgYAMLFile:
		return `:file:_files -g "*.(yaml|yml)"`
	case completionArgAnyFile:
		return ":file:_files"
	case completionArgCommand:
		return ":command:_command_names -e"
	case completionArgShellName:
		return ":shell:(" + strings.Join(completionShellNameList(), " ") + ")"
	case completionArgOpaque:
		return ":value:"
	default:
		return ""
	}
}

func completionZsh(fs []*completionFlag) []string {
	lines := []string{
		"#compdef actionlint",
		"",
		"_actionlint() {",
		"    _arguments \\",
	}

	for _, f := range fs {
		spec := "-" + completionQuoteZsh(f.name) + "[" + completionQuoteZsh(f.desc) + "]" + completionZshAction(f.arg)
		if f.repeatable {
			spec = "*" + spec
		}
		lines = append(lines, "        '"+spec+"' \\")
	}

	return append(
		lines,
		`        '*:workflow file:_files -g "*.(yaml|yml)"'`,
		"}",
		"",
		`if [ "$funcstack[1]" = "_actionlint" ]; then`,
		`    _actionlint "$@"`,
		"else",
		"    compdef _actionlint actionlint",
		"fi",
	)
}

func completionFishArg(a completionArg) string {
	switch a {
	case completionArgYAMLFile:
		return " -x -a '(__actionlint_workflow_files)'"
	case completionArgAnyFile:
		return " -r -F"
	case completionArgCommand:
		return " -x -a '(__fish_complete_command)'"
	case completionArgShellName:
		return " -x -a " + completionQuoteFish(strings.Join(completionShellNameList(), " "))
	case completionArgOpaque:
		return " -x"
	default:
		return ""
	}
}

func completionFish(fs []*completionFlag) []string {
	lines := []string{
		"# fish completion for actionlint",
		"function __actionlint_workflow_files",
		"    set -l token (commandline -ct)",
		"    set -l files $token*.yaml $token*.yml $token*/",
		`    printf '%s\n' $files`,
		"end",
		"complete -c actionlint -f",
	}

	for _, f := range fs {
		line := "complete -c actionlint -o " + completionQuoteFish(f.name) + completionFishArg(f.arg)
		if f.desc != "" {
			line += " -d " + completionQuoteFish(f.desc)
		}
		lines = append(lines, line)
	}

	return append(
		lines,
		"complete -c actionlint -a '(__actionlint_workflow_files)' -d 'Workflow file'",
	)
}

const completionPwshYAMLFiles = `    return Get-ChildItem -Path "$wordToComplete*" -ErrorAction SilentlyContinue |
        Where-Object { $_.PSIsContainer -or $_.Extension -in '.yaml', '.yml' } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ProviderItem', $_.FullName)
        }`

func completionPwshKind(a completionArg) string {
	switch a {
	case completionArgYAMLFile:
		return "yamlfile"
	case completionArgAnyFile:
		return "anyfile"
	case completionArgCommand:
		return "command"
	case completionArgShellName:
		return "shell"
	case completionArgOpaque:
		return "opaque"
	default:
		return "none"
	}
}

func completionPwshResults(a completionArg) []string {
	switch a {
	case completionArgShellName:
		return []string{
			`                return $shells | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {`,
			`                    [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)`,
			"                }",
		}
	case completionArgCommand:
		return []string{
			`                return Get-Command -Name "$wordToComplete*" -ErrorAction SilentlyContinue | ForEach-Object {`,
			`                    [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'Command', $_.Name)`,
			"                }",
		}
	case completionArgAnyFile:
		return []string{
			`                return Get-ChildItem -Path "$wordToComplete*" -ErrorAction SilentlyContinue | ForEach-Object {`,
			`                    [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ProviderItem', $_.FullName)`,
			"                }",
		}
	case completionArgYAMLFile:
		return []string{
			`                return Get-ChildItem -Path "$wordToComplete*" -ErrorAction SilentlyContinue |`,
			`                    Where-Object { $_.PSIsContainer -or $_.Extension -in '.yaml', '.yml' } | ForEach-Object {`,
			`                        [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ProviderItem', $_.FullName)`,
			"                    }",
		}
	default:
		return []string{"                return @()"}
	}
}

func completionPwsh(fs []*completionFlag) []string {
	lines := []string{
		"# PowerShell completion for actionlint",
		"Register-ArgumentCompleter -Native -CommandName actionlint -ScriptBlock {",
		"    param($wordToComplete, $commandAst, $cursorPosition)",
		"",
		"    $flags = @(",
	}
	for _, f := range fs {
		lines = append(
			lines,
			"        [pscustomobject]@{ Name = "+completionQuotePwsh("-"+f.name)+"; Description = "+completionQuotePwsh(f.desc)+" }",
		)
	}

	shells := make([]string, 0, len(completionShells))
	for _, s := range completionShells {
		shells = append(shells, completionQuotePwsh(string(s)))
	}

	lines = append(
		lines,
		"    )",
		"    $shells = @("+strings.Join(shells, ", ")+")",
		"    $takesValue = @{",
	)

	var withValue []*completionFlag
	for _, a := range completionArgOrder {
		matched := completionFlagsWithArg(fs, a)
		withValue = append(withValue, matched...)
		for _, f := range matched {
			lines = append(lines, "        "+completionQuotePwsh(f.name)+" = "+completionQuotePwsh(completionPwshKind(a)))
		}
	}

	lines = append(
		lines,
		"    }",
		"",
		"    $elements = @($commandAst.CommandElements)",
		"    $prev = ''",
		"    if ($elements.Count -ge 2) {",
		"        $last = $elements[$elements.Count - 1].ToString()",
		"        if ($last -eq $wordToComplete) {",
		"            if ($elements.Count -ge 3) { $prev = $elements[$elements.Count - 2].ToString() }",
		"        } else {",
		"            $prev = $last",
		"        }",
		"    }",
		"    $prevName = $prev -replace '^--?', ''",
		"",
	)

	if len(withValue) > 0 {
		lines = append(
			lines,
			"    if ($takesValue.ContainsKey($prevName)) {",
			"        switch ($takesValue[$prevName]) {",
		)
		for _, a := range completionArgOrder {
			if len(completionFlagsWithArg(fs, a)) == 0 {
				continue
			}
			lines = append(lines, "            "+completionQuotePwsh(completionPwshKind(a))+" {")
			lines = append(lines, completionPwshResults(a)...)
			lines = append(lines, "            }")
		}
		lines = append(lines, "        }", "    }", "")
	}

	return append(
		lines,
		"    if ($wordToComplete.StartsWith('-')) {",
		`        return $flags | Where-Object { $_.Name -like "$wordToComplete*" } | ForEach-Object {`,
		`            [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ParameterName', $_.Description)`,
		"        }",
		"    }",
		"",
		completionPwshYAMLFiles,
		"}",
	)
}
