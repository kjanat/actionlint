package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
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
