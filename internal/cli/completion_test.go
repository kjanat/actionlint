package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestCompletionProtocolWithFilename(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("__complete", []byte(commandBadWorkflow), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr strings.Builder
	command := Command{Stdout: &out, Stderr: &stderr, Stdin: strings.NewReader("")}
	if code := command.Main([]string{"actionlint", "__complete", "check", "--output-format", ""}); code != 0 || !strings.Contains(out.String(), "json") {
		t.Fatalf("%d: %s %s", code, &out, &stderr)
	}
	got := testRunCommand("", "--", "__complete")
	if got.Status != 1 || !strings.Contains(got.Stdout, "[expression]") {
		t.Fatal(got)
	}
}

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

func testCheckShellSyntax(t *testing.T, shell completionShell, script string) {
	t.Helper()

	var file string
	switch shell {
	case completionShellBash:
		file = "actionlint.bash"
	case completionShellFish:
		file = "actionlint.fish"
	case completionShellPowerShell:
		file = "actionlint.ps1"
	case completionShellZsh:
		file = "_actionlint"
	default:
		t.Fatalf("unknown shell %q", shell)
	}

	path := filepath.Join(t.TempDir(), file)
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	var candidates []string
	var args []string
	switch shell {
	case completionShellBash:
		candidates = []string{"bash"}
		args = []string{"-n", path}
	case completionShellFish:
		candidates = []string{"fish"}
		args = []string{"--no-config", "--no-execute", path}
	case completionShellPowerShell:
		candidates = []string{"pwsh", "powershell"}
		args = []string{
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			fmt.Sprintf(
				"$e=$null; $null=[System.Management.Automation.Language.Parser]::ParseFile('%s',[ref]$null,[ref]$e); if($e.Count -gt 0){ $e[0].Message; exit 1 }",
				path,
			),
		}
	case completionShellZsh:
		candidates = []string{"zsh"}
		args = []string{"-f", "-n", path}
	}

	var bin string
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		t.Skipf("none of %v is installed", candidates)
	}

	out, err := exec.CommandContext(t.Context(), bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s reported a syntax error in the generated script: %v\n%s\nscript:\n%s", bin, err, out, script)
	}
	if len(out) > 0 {
		t.Fatalf("%s wrote unexpected output while checking the generated script:\n%s\nscript:\n%s", bin, out, script)
	}
}

func TestCompletionShellResolution(t *testing.T) {
	tests := []struct {
		in   string
		want completionShell
		ok   bool
	}{
		{"bash", completionShellBash, true},
		{"fish", completionShellFish, true},
		{"powershell", completionShellPowerShell, true},
		{"zsh", completionShellZsh, true},
		{"pwsh", completionShellPowerShell, true},
		{"/usr/bin/zsh", completionShellZsh, true},
		{"/usr/local/bin/fish", completionShellFish, true},
		{`C:\Program Files\PowerShell\7\pwsh.exe`, completionShellPowerShell, true},
		{"ZSH", completionShellZsh, true},
		{"tcsh", "", false},
		{"/usr/bin/ksh", "", false},
		{"", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			have, ok := completionShellFromPath(tc.in)
			if ok != tc.ok || have != tc.want {
				t.Fatalf("completionShellFromPath(%q) = (%q, %v) but wanted (%q, %v)", tc.in, have, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCompletionShellDetection(t *testing.T) {
	tests := []struct {
		what     string
		shellVar string
		psmp     string
		want     completionShell
		ok       bool
	}{
		{"shell variable wins", "/bin/bash", "", completionShellBash, true},
		{"shell variable wins over the fallback", "/bin/bash", "/some/modules", completionShellBash, true},
		{"psmodulepath fallback without shell variable", "", "/some/modules", completionShellPowerShell, true},
		{"psmodulepath fallback on an unsupported shell", "/usr/bin/ksh", "/some/modules", completionShellPowerShell, true},
		{"no signal", "", "", "", false},
		{"unsupported shell without fallback", "/usr/bin/ksh", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			have, ok := detectCompletionShell(tc.shellVar, tc.psmp)
			if ok != tc.ok || have != tc.want {
				t.Fatalf("detectCompletionShell(%q, %q) = (%q, %v) but wanted (%q, %v)", tc.shellVar, tc.psmp, have, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCommandCompletionAuto(t *testing.T) {
	t.Run("shell from environment", func(t *testing.T) {
		t.Setenv("SHELL", "/usr/bin/fish")
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		cmd := &Command{Stdin: bytes.NewReader(nil), Stdout: stdout, Stderr: stderr}
		if status := cmd.Main([]string{"actionlint", "-completion", "auto"}); status != actionlint.ExitStatusSuccessNoProblem {
			t.Fatalf("exit status is %d. stderr:\n%s", status, stderr)
		}
		if !strings.Contains(stdout.String(), "# fish completion for actionlint") {
			t.Errorf("stdout is not the fish script:\n%s", stdout)
		}
	})

	t.Run("no signal", func(t *testing.T) {
		t.Setenv("SHELL", "")
		t.Setenv("PSModulePath", "")
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		cmd := &Command{Stdin: bytes.NewReader(nil), Stdout: stdout, Stderr: stderr}
		if status := cmd.Main([]string{"actionlint", "-completion", "auto"}); status != actionlint.ExitStatusInvalidCommandOption {
			t.Fatalf("exit status is %d. stdout:\n%s", status, stdout)
		}
		if !strings.Contains(stderr.String(), "cannot detect the current shell") {
			t.Errorf("stderr does not explain the detection failure:\n%s", stderr)
		}
	})
}

func testCompletionScript(t *testing.T, shell completionShell) string {
	t.Helper()
	app := newCommandApp(&Command{Stdout: io.Discard, Stderr: io.Discard})
	var b strings.Builder
	if err := writeCompletion(&b, shell, app.root); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestCompletionScriptSyntax(t *testing.T) {
	for _, shell := range completionShells {
		t.Run(string(shell), func(t *testing.T) { testCheckShellSyntax(t, shell, testCompletionScript(t, shell)) })
	}
}

func TestCommandCompletionFlag(t *testing.T) {
	for _, flag := range []string{"-completion", "--completion", "-completions", "--completions"} {
		for _, shell := range completionShells {
			t.Run(flag+"/"+string(shell), func(t *testing.T) {
				var out, errout bytes.Buffer
				cmd := Command{Stdout: &out, Stderr: &errout}
				if status := cmd.Main([]string{"actionlint", flag, string(shell), "missing.yml"}); status != 0 {
					t.Fatalf("status=%d, stderr=%s", status, &errout)
				}
				if errout.Len() != 0 || out.String() != testCompletionScript(t, shell) {
					t.Fatalf("completion alias changed generated output: stderr=%s", &errout)
				}
			})
		}
	}
}

func TestCompletionProtocol(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		want   []string
		absent []string
	}{
		{"flags", []string{"--j"}, []string{"--json", ":4"}, nil},
		{"version shorthand", []string{"-V"}, []string{"-V", ":4"}, nil},
		{"output choices", []string{"--output", "j"}, []string{"text", "oneline", "json", "jsonl", "sarif", ":4"}, nil},
		{"legacy flag values", []string{"-completion", ""}, []string{"bash", "fish", "powershell", "zsh"}, nil},
		{"plural alias values", []string{"--completions", ""}, []string{"bash", "powershell"}, nil},
		{"config files", []string{"--config-file", ""}, []string{"yaml", "yml", ":8"}, nil},
		{"opaque templates", []string{"--format", ""}, []string{":4"}, []string{"--json"}},
		{"flags after files", []string{"workflow.yml", "--j"}, []string{":8"}, []string{"--json"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errout bytes.Buffer
			cmd := Command{Stdout: &out, Stderr: &errout}
			args := append([]string{"actionlint", "__complete"}, tc.args...)
			if code := cmd.Main(args); code != 0 {
				t.Fatalf("exit=%d, stderr=%s", code, &errout)
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("completion missing %q: %s", want, &out)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("unexpected completion %q: %s", absent, &out)
				}
			}
		})
	}
}
