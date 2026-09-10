package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

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
