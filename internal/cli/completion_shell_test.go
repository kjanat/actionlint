package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestCompletionShellBehaviour(t *testing.T) {
	binDir := t.TempDir()
	exe := filepath.Join(binDir, "actionlint")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", exe, "./cmd/actionlint")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	tests := []struct {
		shell  completionShell
		bin    string
		driver string
		args   []string
	}{
		{completionShellPowerShell, "pwsh", `param($Script)
. $Script
Set-PSReadLineKeyHandler -Key Tab -Function TabCompleteNext
foreach ($Line in @('actionlint --co', 'actionlint --output j', 'actionlint -completion z',
  'actionlint check workflow.yml --output-format j', 'actionlint config show --or',
  'actionlint completion z', 'actionlint rules express')) {
  $r = TabExpansion2 -inputScript $Line -cursorColumn $Line.Length
  Write-Output (@($r.CompletionMatches | ForEach-Object { $_.CompletionText }) -join '|')
}
# PowerShell/PowerShell#27398 fixes TabExpansion2 throwing for an empty result.
# Inspect that result directly; PSReadLine suppresses it on older engines.
$Line = 'actionlint --ignore '
$Ast = [System.Management.Automation.Language.Parser]::ParseInput($Line, [ref]$null, [ref]$null)
$CommandAst = $Ast.Find({ param($Node) $Node -is [System.Management.Automation.Language.CommandAst] }, $true)
$EmptyResult = @(& $__actionlintCompleterBlock '' $CommandAst $Line.Length)
if ($EmptyResult.Count -ne 1 -or $EmptyResult[0] -ne '') { throw 'Expected the no-file-completion result' }
Write-Output ''
`, []string{"-NoProfile", "-NonInteractive", "-File"}},
		{completionShellFish, "fish", `source $argv[1]
for line in 'actionlint --co' 'actionlint --output j' 'actionlint -completion z' \
    'actionlint check workflow.yml --output-format j' 'actionlint config show --or' \
    'actionlint completion z' 'actionlint rules express' 'actionlint --ignore '
  set results (complete -C "$line" | string split -f1 \t)
  echo (string join -- '|' $results)
end
`, []string{"--no-config"}},
		{completionShellBash, "bash", `source "$BASH_COMPLETION_FILE"
# compopt changes interactive Readline options; the driver checks candidates.
compopt() { :; }
source "$1"
run() {
  COMP_WORDS=("$@")
  COMP_CWORD=$(( ${#COMP_WORDS[@]} - 1 ))
  COMP_LINE="${COMP_WORDS[*]}"
  COMP_POINT=${#COMP_LINE}
  COMPREPLY=()
  __start_actionlint
  local IFS='|'
  printf '%s\n' "${COMPREPLY[*]}"
}
run actionlint --co
run actionlint --output j
run actionlint -completion z
run actionlint check workflow.yml --output-format j
run actionlint config show --or
run actionlint completion z
run actionlint rules express
run actionlint --ignore ''
`, []string{"--noprofile", "--norc"}},
	}
	want := [][]string{{"--color", "--config"}, {"json", "jsonl"}, {"zsh"}, {"json", "jsonl"}, {"--origin"}, {"zsh"}, {"expression"}, nil}
	for _, tc := range tests {
		t.Run(string(tc.shell), func(t *testing.T) {
			if runtime.GOOS == "windows" && tc.shell != completionShellPowerShell {
				t.Skip("POSIX shell driver")
			}
			bin, err := exec.LookPath(tc.bin)
			if err != nil {
				t.Skip(tc.bin + " is not installed")
			}
			bashCompletion := os.Getenv("BASH_COMPLETION_FILE")
			if tc.shell == completionShellBash {
				if bashCompletion == "" {
					bashCompletion = "/usr/share/bash-completion/bash_completion"
				}
				if _, err := os.Stat(bashCompletion); err != nil {
					t.Skip("bash-completion is not installed")
				}
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "unwanted.txt"), []byte(""), 0o600); err != nil {
				t.Fatal(err)
			}
			script := filepath.Join(dir, "completion.ps1")
			driver := filepath.Join(dir, "driver.ps1")
			if err := os.WriteFile(script, []byte(testCompletionScript(t, tc.shell)), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(driver, []byte(tc.driver), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), bin, append(slices.Clone(tc.args), driver, script)...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "BASH_COMPLETION_FILE="+bashCompletion)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("shell failed: %v\n%s", err, out)
			}
			lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n"), "\n")
			if len(lines) != len(want) {
				t.Fatalf("expected %d results, got %q", len(want), lines)
			}
			for i, line := range lines {
				var got []string
				if line != "" {
					got = strings.Split(line, "|")
					slices.Sort(got)
				}
				if !slices.Equal(got, want[i]) {
					t.Errorf("case %d: got %q, want %q", i, got, want[i])
				}
			}
		})
	}
}

func TestManualDocumentsEveryFlag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("man", "actionlint.1.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n# FLAGS\n")
	if !ok {
		t.Fatal("manual has no FLAGS section")
	}
	body, _, _ = strings.Cut(body, "\n# ")
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile(`\*\*(--?[^*]+)\*\*`).FindAllStringSubmatch(body, -1) {
		documented[m[1]] = true
	}
	app := newCommandApp(&Command{})
	app.root.Flags().VisitAll(func(f *pflag.Flag) {
		for _, name := range []string{"--" + f.Name, "-" + f.Shorthand} {
			if name == "-" {
				continue
			}
			if !documented[name] {
				t.Errorf("manual is missing %s", name)
			}
			delete(documented, name)
		}
	})
	for name := range documented {
		t.Errorf("manual documents unknown flag %s", name)
	}
}
