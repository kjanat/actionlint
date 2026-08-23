package actionlint

import (
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
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

var completionShellAliases = map[string]completionShell{
	"pwsh": completionShellPowerShell,
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

func completionShellFromName(name string) (completionShell, bool) {
	for _, c := range completionShells {
		if completionShell(name) == c {
			return c, true
		}
	}
	if c, ok := completionShellAliases[name]; ok {
		return c, true
	}
	return "", false
}

// completionShellFromPath resolves a bare shell name such as "zsh" or "pwsh", or a shell executable
// path such as "/usr/bin/zsh" or `C:\Program Files\PowerShell\7\pwsh.exe`, to the shell it names.
// Accepting a path makes `actionlint -completion "$SHELL"` work as-is.
func completionShellFromPath(p string) (completionShell, bool) {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	p = strings.TrimSuffix(strings.ToLower(p), ".exe")
	return completionShellFromName(p)
}

// detectCompletionShell resolves the current shell from the process environment. $SHELL wins when
// it names a supported shell. PowerShell exports $PSModulePath but never $SHELL, so a non-empty
// $PSModulePath is the fallback signal. A pwsh session launched from bash inherits SHELL=/bin/bash
// and the login shell the user opted into wins over the fallback.
func detectCompletionShell(shellVar, psModulePath string) (completionShell, bool) {
	if s, ok := completionShellFromPath(shellVar); ok {
		return s, true
	}
	if psModulePath != "" {
		return completionShellPowerShell, true
	}
	return "", false
}

func (s *completionShell) String() string {
	return string(*s)
}

func (s *completionShell) Set(v string) error {
	if v == "auto" {
		c, ok := detectCompletionShell(os.Getenv("SHELL"), os.Getenv("PSModulePath"))
		if !ok {
			return fmt.Errorf("cannot detect the current shell from the environment. Set $SHELL or name one of %s", completionShellNames())
		}
		*s = c
		return nil
	}
	if c, ok := completionShellFromPath(v); ok {
		*s = c
		return nil
	}
	return fmt.Errorf("must be one of %s, \"pwsh\", a path to one of them, or \"auto\"", completionShellNames())
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

const completionHelpDescription = "Show this help message"

func completionFlagsOf(flags *flag.FlagSet) []*completionFlag {
	// -h and -help are handled by the flag package itself so VisitAll does not see them.
	fs := []*completionFlag{
		{name: "h", desc: completionHelpDescription},
		{name: "help", desc: completionHelpDescription},
	}
	flags.VisitAll(func(f *flag.Flag) {
		_, repeatable := f.Value.(repeatableFlag)
		fs = append(fs, &completionFlag{
			name:       f.Name,
			desc:       completionDescription(f),
			arg:        completionArgOf(f),
			repeatable: repeatable,
		})
	})
	slices.SortFunc(fs, func(a, b *completionFlag) int { return strings.Compare(a.name, b.name) })
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

const completionBashYAMLFiles = `__actionlint_reply < <(compgen -d -- "$cur"; compgen -f -X '!*.yaml' -- "$cur"; compgen -f -X '!*.yml' -- "$cur")`

func completionBashReply(a completionArg) string {
	switch a {
	case completionArgYAMLFile:
		return completionBashYAMLFiles
	case completionArgAnyFile:
		return `__actionlint_reply < <(compgen -f -- "$cur")`
	case completionArgCommand:
		return `__actionlint_reply < <(compgen -c -- "$cur")`
	case completionArgShellName:
		return `__actionlint_reply < <(compgen -W "$__shells" -- "$cur")`
	default:
		return "COMPREPLY=()"
	}
}

func completionBash(fs []*completionFlag) []string {
	names := make([]string, 0, len(fs))
	doubles := make([]string, 0, len(fs))
	for _, f := range fs {
		names = append(names, "-"+f.name)
		doubles = append(doubles, "--"+f.name)
	}

	lines := []string{
		"# bash completion for actionlint",
		"",
		// COMPREPLY=( $(compgen ...) ) would run word splitting and pathname expansion over the
		// candidates, so a file named "my file.yaml" splits in two and "q*.yaml" globs.
		"__actionlint_reply() {",
		"    COMPREPLY=()",
		"    local line",
		"    while IFS= read -r line; do",
		`        COMPREPLY+=("$line")`,
		"    done",
		"}",
		"",
		"_actionlint() {",
		"    local cur prev flagword",
		"    local __flags=" + completionQuoteBash(strings.Join(names, "\n")),
		"    local __dflags=" + completionQuoteBash(strings.Join(doubles, "\n")),
		"    local __shells=" + completionQuoteBash(strings.Join(completionShellNameList(), "\n")),
		`    cur="${COMP_WORDS[COMP_CWORD]}"`,
		`    prev="${COMP_WORDS[COMP_CWORD-1]}"`,
		"",
		// COMP_WORDBREAKS contains '=', so "-flag=value" arrives as the three words
		// "-flag", "=", "value". Readline replaces only the text after "=".
		`    flagword="$prev"`,
		`    if [[ "$cur" == '=' && "$prev" == -* ]]; then`,
		`        flagword="$prev"`,
		"        cur=''",
		`    elif [[ "$prev" == '=' && $COMP_CWORD -ge 2 && "${COMP_WORDS[COMP_CWORD-2]}" == -* ]]; then`,
		`        flagword="${COMP_WORDS[COMP_CWORD-2]}"`,
		"    fi",
		"",
		`    case "$flagword" in`,
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
		`    if [[ "$cur" == --* ]]; then`,
		`        __actionlint_reply < <(compgen -W "$__dflags" -- "$cur")`,
		"        return 0",
		"    fi",
		`    if [[ "$cur" == -* ]]; then`,
		`        __actionlint_reply < <(compgen -W "$__flags" -- "$cur")`,
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

// completionZshSpecs returns the _arguments specs for one flag: the single-dash form Go documents
// and the double-dash form the flag package also accepts. A trailing "=" on the option name lets
// _arguments complete the "-flag=value" spelling as well as the separate word.
func completionZshSpecs(f *completionFlag) []string {
	eq := ""
	if f.arg != completionArgNone {
		eq = "="
	}
	name := completionQuoteZsh(f.name)
	tail := eq + "[" + completionQuoteZsh(f.desc) + "]" + completionZshAction(f.arg)
	if f.repeatable {
		return []string{
			"*-" + name + tail,
			"*--" + name + tail,
		}
	}
	return []string{
		"(--" + name + ")-" + name + tail,
		"(-" + name + ")--" + name + tail,
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
		for _, spec := range completionZshSpecs(f) {
			lines = append(lines, "        '"+spec+"' \\")
		}
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

// completionFishOptions spells one flag for fish: "-s" for a one-letter name, and both the
// old-style "-o" and the GNU-style "-l" spellings otherwise, so "-color" and "--color" complete.
func completionFishOptions(f *completionFlag) string {
	name := completionQuoteFish(f.name)
	if len(f.name) == 1 {
		return "-s " + name
	}
	return "-o " + name + " -l " + name
}

func completionFish(fs []*completionFlag) []string {
	lines := []string{
		"# fish completion for actionlint",
		"function __actionlint_workflow_files",
		// Expanding the raw token as a glob prefix breaks on quotes and escapes, so ask fish's own
		// file completion for candidates and keep the workflow files and directories.
		"    set -l token (commandline -ct | string replace -r -- '^-[^=]*=' '')",
		`    set -l files (complete -C"__fish_command_without_completions $token")`,
		`    set -l matched (string match -r -- '^.*\.(?:yaml|yml)$' $files)`,
		"    set -l dirs (string match -r -- '^.*/$' $files)",
		"    set files $matched $dirs",
		"    if set -q files[1]",
		`        printf '%s\n' $files`,
		"    end",
		"end",
		"complete -c actionlint -f",
	}

	for _, f := range fs {
		line := "complete -c actionlint " + completionFishOptions(f) + completionFishArg(f.arg)
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

const completionPwshYAMLFiles = `Get-ChildItem -Path "$word*" -ErrorAction SilentlyContinue |
                    Where-Object { $_.PSIsContainer -or $_.Extension -in '.yaml', '.yml' } | ForEach-Object {
                        __ActionlintResult ($prefix + $dir + $_.Name) 'ProviderItem' $_.FullName
                    }`

func completionPwshResults(a completionArg) []string {
	switch a {
	case completionArgShellName:
		return []string{
			`                return $shells | Where-Object { $_ -like "$word*" } | ForEach-Object {`,
			`                    __ActionlintResult ($prefix + $_) 'ParameterValue' $_`,
			"                }",
		}
	case completionArgCommand:
		return []string{
			`                return Get-Command -Name "$word*" -ErrorAction SilentlyContinue | ForEach-Object {`,
			`                    __ActionlintResult ($prefix + $_.Name) 'Command' $_.Name`,
			"                }",
		}
	case completionArgAnyFile:
		return []string{
			`                return Get-ChildItem -Path "$word*" -ErrorAction SilentlyContinue | ForEach-Object {`,
			`                    __ActionlintResult ($prefix + $dir + $_.Name) 'ProviderItem' $_.FullName`,
			"                }",
		}
	case completionArgYAMLFile:
		return []string{
			"                return " + completionPwshYAMLFiles,
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
			"        [pscustomobject]@{ Name = "+completionQuotePwsh(f.name)+"; Description = "+completionQuotePwsh(f.desc)+" }",
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
		// The returned CompletionText replaces the word as-is, so a candidate holding a space or
		// another argument-splitting character must be quoted here.
		"    function __ActionlintResult([string]$text, [string]$type, [string]$tooltip) {",
		"        $item = $text",
		`        if ($text -match '[^\w\-./\\:~=]') { $text = "'" + ($text -replace "'", "''") + "'" }`,
		"        [System.Management.Automation.CompletionResult]::new($text, $item, $type, $tooltip)",
		"    }",
		"",
		`    $word = "$wordToComplete"`,
		"    $prefix = ''",
		"    $prev = ''",
		"    if ($word -match '^(--?[^=]+)=(.*)$') {",
		"        $prev = $Matches[1]",
		"        $prefix = $Matches[1] + '='",
		"        $word = $Matches[2]",
		"    } else {",
		"        $elements = @($commandAst.CommandElements)",
		"        if ($elements.Count -ge 2) {",
		"            $last = $elements[$elements.Count - 1].ToString()",
		"            if ($last -eq $word) {",
		"                if ($elements.Count -ge 3) { $prev = $elements[$elements.Count - 2].ToString() }",
		"            } else {",
		"                $prev = $last",
		"            }",
		"        }",
		"    }",
		"    $prevName = $prev -replace '^--?', ''",
		"",
		// Get-ChildItem results carry only their leaf name, so completing "sub/d" must put the
		// directory the user already typed back in front of the candidate.
		"    $dir = ''",
		`    $m = [regex]::Match($word, '^(.*[/\\])')`,
		"    if ($m.Success) { $dir = $m.Groups[1].Value }",
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
		"    if ($word.StartsWith('-')) {",
		"        $dash = '-'",
		"        if ($word.StartsWith('--')) { $dash = '--' }",
		"        $stem = $word -replace '^--?', ''",
		`        return $flags | Where-Object { $_.Name -like "$stem*" } | ForEach-Object {`,
		"            __ActionlintResult ($dash + $_.Name) 'ParameterName' $_.Description",
		"        }",
		"    }",
		"",
		`    return Get-ChildItem -Path "$word*" -ErrorAction SilentlyContinue |`,
		"        Where-Object { $_.PSIsContainer -or $_.Extension -in '.yaml', '.yml' } | ForEach-Object {",
		"            __ActionlintResult ($dir + $_.Name) 'ProviderItem' $_.FullName",
		"        }",
		"}",
	)
}
