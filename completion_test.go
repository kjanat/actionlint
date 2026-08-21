package actionlint

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A fish completion script spells an option as `-o color`, where the dash belongs to fish's own
// -o flag. Every other shell spells it `-color`.
func testCompletionFlagToken(shell completionShell, name string) string {
	if shell == completionShellFish {
		return "-o " + name
	}
	return "-" + name
}

func testCompletionScript(t *testing.T, shell completionShell) string {
	t.Helper()
	var f commandFlags
	flags := f.newFlagSet("actionlint", io.Discard)
	var b strings.Builder
	if err := writeCompletion(&b, shell, flags); err != nil {
		t.Fatalf("writing %s completion script failed: %v", shell, err)
	}
	return b.String()
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

func TestCompletionScriptContainsEveryFlag(t *testing.T) {
	for _, shell := range completionShells {
		t.Run(string(shell), func(t *testing.T) {
			script := testCompletionScript(t, shell)
			var f commandFlags
			flags := f.newFlagSet("actionlint", io.Discard)
			flags.VisitAll(func(fl *flag.Flag) {
				token := testCompletionFlagToken(shell, fl.Name)
				if !strings.Contains(script, token) {
					t.Errorf("flag %q is missing from the %s completion script. Expected the token %q:\n%s", "-"+fl.Name, shell, token, script)
				}
			})
		})
	}
}

func TestCompletionArgTableCoversEveryFlag(t *testing.T) {
	var f commandFlags
	flags := f.newFlagSet("actionlint", io.Discard)
	flags.VisitAll(func(fl *flag.Flag) {
		a, ok := completionArgs[fl.Name]
		if !ok {
			t.Errorf("flag %q has no entry in completionArgs. Add one so the completion scripts know what its value completes to", "-"+fl.Name)
			return
		}
		typeName, _ := flag.UnquoteUsage(fl)
		isBool := typeName == ""
		if isBool != (a == completionArgNone) {
			t.Errorf(
				"flag %q takes no value = %v, but its completionArgs entry says completionArgNone = %v",
				"-"+fl.Name, isBool, a == completionArgNone,
			)
		}
	})
}

func TestCompletionScriptMarker(t *testing.T) {
	tests := []struct {
		shell  completionShell
		marker string
	}{
		{completionShellBash, "complete -o filenames -F _actionlint actionlint"},
		{completionShellFish, "complete -c actionlint"},
		{completionShellPowerShell, "Register-ArgumentCompleter -Native -CommandName actionlint"},
		{completionShellZsh, "#compdef actionlint"},
	}

	for _, tc := range tests {
		t.Run(string(tc.shell), func(t *testing.T) {
			script := testCompletionScript(t, tc.shell)
			if !strings.Contains(script, tc.marker) {
				t.Fatalf("%s completion script does not register itself with %q:\n%s", tc.shell, tc.marker, script)
			}
		})
	}
}

func TestCompletionScriptSyntax(t *testing.T) {
	for _, shell := range completionShells {
		t.Run(string(shell), func(t *testing.T) {
			testCheckShellSyntax(t, shell, testCompletionScript(t, shell))
		})
	}
}

func TestCompletionDescriptionEscaping(t *testing.T) {
	for _, shell := range completionShells {
		t.Run(string(shell), func(t *testing.T) {
			var b bool
			flags := flag.NewFlagSet("actionlint", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			flags.BoolVar(&b, "we'ird", false, "quote ' double \" back \\ brack [ ] colon : dollar $ tick ` newline\nend")

			var out strings.Builder
			if err := writeCompletion(&out, shell, flags); err != nil {
				t.Fatalf("writing %s completion script failed: %v", shell, err)
			}
			testCheckShellSyntax(t, shell, out.String())
		})
	}
}

func TestCommandCompletionFlag(t *testing.T) {
	tests := []struct {
		what   string
		args   []string
		status int
		stdout string
		stderr string
	}{
		{
			what:   "bash",
			args:   []string{"actionlint", "-completion", "bash"},
			status: ExitStatusSuccessNoProblem,
			stdout: "complete -o filenames -F _actionlint actionlint",
		},
		{
			what:   "unknown shell",
			args:   []string{"actionlint", "-completion", "tcsh"},
			status: ExitStatusInvalidCommandOption,
			stderr: `invalid value "tcsh" for flag -completion:`,
		},
		{
			what:   "missing value",
			args:   []string{"actionlint", "-completion"},
			status: ExitStatusInvalidCommandOption,
			stderr: "flag needs an argument: -completion",
		},
		{
			what:   "positional arguments are ignored",
			args:   []string{"actionlint", "-completion", "fish", "testdata/examples/main.yaml"},
			status: ExitStatusSuccessNoProblem,
			stdout: "complete -c actionlint",
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			cmd := &Command{Stdin: bytes.NewReader(nil), Stdout: stdout, Stderr: stderr}

			status := cmd.Main(tc.args)
			if status != tc.status {
				t.Fatalf("exit status is %d but wanted %d. stdout:\n%s\nstderr:\n%s", status, tc.status, stdout, stderr)
			}
			if tc.stdout != "" && !strings.Contains(stdout.String(), tc.stdout) {
				t.Errorf("stdout does not contain %q:\n%s", tc.stdout, stdout)
			}
			if tc.stderr == "" {
				if stderr.Len() > 0 {
					t.Errorf("stderr is not empty:\n%s", stderr)
				}
			} else if !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stderr does not contain %q:\n%s", tc.stderr, stderr)
			}
			if tc.status == ExitStatusSuccessNoProblem && strings.Contains(stdout.String(), ".yaml:") {
				t.Errorf("stdout contains lint errors although -completion was given:\n%s", stdout)
			}
		})
	}
}

func TestCompletionDescription(t *testing.T) {
	tests := []struct {
		flag string
		want string
	}{
		{"oneline", "Use one line per one error"},
		{"shellcheck", `Command line of "shellcheck" external command`},
		{"init-config", "Generate default config file at .github/actionlint.yaml in current project"},
	}

	var f commandFlags
	flags := f.newFlagSet("actionlint", io.Discard)

	for _, tc := range tests {
		t.Run(tc.flag, func(t *testing.T) {
			fl := flags.Lookup(tc.flag)
			if fl == nil {
				t.Fatalf("flag %q does not exist", "-"+tc.flag)
			}
			if have := completionDescription(fl); have != tc.want {
				t.Fatalf("description is %q but wanted %q", have, tc.want)
			}
		})
	}
}

func TestManualDocumentsEveryFlag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("man", "actionlint.1.md"))
	if err != nil {
		t.Fatal(err)
	}
	man := string(b)

	_, body, ok := strings.Cut(man, "\n# FLAGS\n")
	if !ok {
		t.Fatal("man/actionlint.1.md has no FLAGS section")
	}
	body, _, _ = strings.Cut(body, "\n# ")

	documented := map[string]struct{}{}
	for _, m := range regexp.MustCompile(`\*\*-([^*]+)\*\*`).FindAllStringSubmatch(body, -1) {
		documented[m[1]] = struct{}{}
	}
	// -h and -help are added by the flag package itself, not by newFlagSet.
	for _, n := range []string{"h", "help"} {
		if _, ok := documented[n]; !ok {
			t.Errorf("flag %q is not documented in the FLAGS section of man/actionlint.1.md", "-"+n)
		}
		delete(documented, n)
	}

	var f commandFlags
	flags := f.newFlagSet("actionlint", io.Discard)
	flags.VisitAll(func(fl *flag.Flag) {
		if _, ok := documented[fl.Name]; !ok {
			t.Errorf("flag %q is not documented in the FLAGS section of man/actionlint.1.md", "-"+fl.Name)
		}
		delete(documented, fl.Name)
	})

	for n := range documented {
		t.Errorf("flag %q is documented in the FLAGS section of man/actionlint.1.md but does not exist in the command", "-"+n)
	}
}
