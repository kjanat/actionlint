package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Commands and input paths share the first position. Extension-filter directives
// would make the shell interpret Cobra's command candidates as file extensions.
func completeRootInputs(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}
	directory, name := filepath.Split(prefix)
	readDir := directory
	if readDir == "" {
		readDir = "."
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var candidates []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), name) {
			continue
		}
		candidate := directory + entry.Name()
		if entry.IsDir() {
			candidate += string(filepath.Separator)
		} else if ext := filepath.Ext(candidate); ext != ".yaml" && ext != ".yml" {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, cobra.ShellCompDirectiveNoFileComp
}

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
	pathExt := os.Getenv("PATHEXT")
	if pathExt == "" {
		// Match os/exec's Windows fallback when PATHEXT is unset or empty.
		pathExt = ".COM;.EXE;.BAT;.CMD"
	}
	ext := filepath.Ext(p)
	for suffix := range strings.SplitSeq(pathExt, ";") {
		suffix = strings.TrimSpace(suffix)
		if suffix == "" {
			continue
		}
		if !strings.HasPrefix(suffix, ".") {
			suffix = "." + suffix
		}
		if strings.EqualFold(ext, suffix) {
			p = strings.TrimSuffix(p, ext)
			break
		}
	}
	return completionShellFromName(strings.ToLower(p))
}

// detectCompletionShell resolves the current shell from the process environment. $SHELL wins when
// it names a supported shell. PowerShell exports $PSModulePath but never $SHELL, so a present
// $PSModulePath is the fallback signal. A pwsh session launched from bash inherits SHELL=/bin/bash
// and the login shell the user opted into wins over the fallback.
func detectCompletionShell(shellVar string, powerShellPresent bool) (completionShell, bool) {
	if s, ok := completionShellFromPath(shellVar); ok {
		return s, true
	}
	if powerShellPresent {
		return completionShellPowerShell, true
	}
	return "", false
}

func currentCompletionShell() (completionShell, bool) {
	_, powerShellPresent := os.LookupEnv("PSModulePath")
	return detectCompletionShell(os.Getenv("SHELL"), powerShellPresent)
}

func completionExamples(shell completionShell) string {
	switch shell {
	case completionShellBash:
		return "  source <(actionlint completion bash)\n  actionlint completion bash > actionlint.bash"
	case completionShellZsh:
		return "  source <(actionlint completion zsh)\n  actionlint completion zsh > /path/on/fpath/_actionlint"
	case completionShellFish:
		return "  actionlint completion fish | source\n  actionlint completion fish > actionlint.fish"
	case completionShellPowerShell:
		return "  actionlint completion powershell | Out-String | Invoke-Expression\n  actionlint completion powershell | Out-File -Encoding utf8 actionlint-completion.ps1"
	default:
		return "  actionlint completion bash\n  actionlint completion zsh\n  actionlint completion fish\n  actionlint completion powershell"
	}
}

func (s *completionShell) String() string {
	return string(*s)
}

func (s *completionShell) Set(v string) error {
	if v == "auto" {
		c, ok := currentCompletionShell()
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

func (s *completionShell) Type() string { return "shell" }

func writeCompletion(out io.Writer, shell completionShell, command *cobra.Command) error {
	switch shell {
	case completionShellBash:
		return command.GenBashCompletionV2(out, true)
	case completionShellZsh:
		return command.GenZshCompletion(out)
	case completionShellFish:
		return command.GenFishCompletion(out, true)
	case completionShellPowerShell:
		return command.GenPowerShellCompletionWithDesc(out)
	default:
		return fmt.Errorf("unsupported completion shell %q", shell)
	}
}
