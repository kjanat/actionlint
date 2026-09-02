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
	"runtime"
	"slices"
	"strings"
	"testing"
)

// A fish completion script spells an option as `-o color -l color`, where the dashes belong to
// fish's own flags, and a one-letter option as `-s h`. Every other shell spells out `-color` and
// `--color` themselves.
func testCompletionFlagTokens(shell completionShell, name string) []string {
	switch shell {
	case completionShellFish:
		if len(name) == 1 {
			return []string{"-s " + name}
		}
		return []string{"-o " + name, "-l " + name}
	case completionShellPowerShell:
		// The PowerShell script stores bare names and prepends the dashes the user typed.
		return []string{"Name = '" + name + "'"}
	default:
		return []string{"-" + name, "--" + name}
	}
}

// testCompletionTokenPattern matches token in a script only when neither side continues with a
// word or dash character, so the token "-color" does not match inside "-no-color".
func testCompletionTokenPattern(token string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^-\w])` + regexp.QuoteMeta(token) + `($|[^-\w])`)
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
			names := []string{"h", "help"}
			flags.VisitAll(func(fl *flag.Flag) {
				names = append(names, fl.Name)
			})
			for _, name := range names {
				for _, token := range testCompletionFlagTokens(shell, name) {
					if !testCompletionTokenPattern(token).MatchString(script) {
						t.Errorf("flag %q is missing from the %s completion script. Expected the token %q:\n%s", "-"+name, shell, token, script)
					}
				}
			}
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

func TestCompletionQuoting(t *testing.T) {
	tests := []struct {
		in   string
		bash string
		zsh  string
		fish string
		pwsh string
	}{
		{
			in:   "plain",
			bash: `'plain'`,
			zsh:  "plain",
			fish: "plain",
			pwsh: `'plain'`,
		},
		{
			in:   "we'ird",
			bash: `'we'\''ird'`,
			zsh:  `we'\''ird`,
			fish: `'we\'ird'`,
			pwsh: `'we''ird'`,
		},
		{
			in:   "a[b]c:d",
			bash: `'a[b]c:d'`,
			zsh:  `a\[b\]c\:d`,
			fish: `'a[b]c:d'`,
			pwsh: `'a[b]c:d'`,
		},
		{
			in:   `back\slash`,
			bash: `'back\slash'`,
			zsh:  `back\\slash`,
			fish: `'back\\slash'`,
			pwsh: `'back\slash'`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if have := completionQuoteBash(tc.in); have != tc.bash {
				t.Errorf("bash quoting is %q but wanted %q", have, tc.bash)
			}
			if have := completionQuoteZsh(tc.in); have != tc.zsh {
				t.Errorf("zsh quoting is %q but wanted %q", have, tc.zsh)
			}
			if have := completionQuoteFish(tc.in); have != tc.fish {
				t.Errorf("fish quoting is %q but wanted %q", have, tc.fish)
			}
			if have := completionQuotePwsh(tc.in); have != tc.pwsh {
				t.Errorf("pwsh quoting is %q but wanted %q", have, tc.pwsh)
			}
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
		{
			what:   "pwsh alias",
			args:   []string{"actionlint", "-completion", "pwsh"},
			status: ExitStatusSuccessNoProblem,
			stdout: "Register-ArgumentCompleter -Native -CommandName actionlint",
		},
		{
			what:   "completions alias",
			args:   []string{"actionlint", "-completions", "bash"},
			status: ExitStatusSuccessNoProblem,
			stdout: "complete -o filenames -F _actionlint actionlint",
		},
		{
			what:   "completions alias with double dash and equals sign",
			args:   []string{"actionlint", "--completions=zsh"},
			status: ExitStatusSuccessNoProblem,
			stdout: "#compdef actionlint",
		},
		{
			what:   "completions alias without a value names the canonical flag",
			args:   []string{"actionlint", "-completions"},
			status: ExitStatusInvalidCommandOption,
			stderr: "flag needs an argument: -completion",
		},
		{
			what:   "shell path",
			args:   []string{"actionlint", "-completion", "/usr/bin/zsh"},
			status: ExitStatusSuccessNoProblem,
			stdout: "#compdef actionlint",
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

func TestCompletionAliasArgs(t *testing.T) {
	tests := []struct {
		what string
		in   []string
		want []string
	}{
		{"single dash", []string{"-completions", "bash"}, []string{"-completion", "bash"}},
		{"double dash", []string{"--completions", "bash"}, []string{"--completion", "bash"}},
		{"equals sign", []string{"-completions=zsh"}, []string{"-completion=zsh"}},
		{"double dash with equals sign", []string{"--completions=zsh"}, []string{"--completion=zsh"}},
		{"canonical spelling is untouched", []string{"-completion", "bash"}, []string{"-completion", "bash"}},
		{"longer flag is untouched", []string{"-completionsx"}, []string{"-completionsx"}},
		{"no rewrite after the terminator", []string{"--", "-completions"}, []string{"--", "-completions"}},
		{"other arguments are untouched", []string{"-verbose", "a.yaml"}, []string{"-verbose", "a.yaml"}},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			if have := completionAliasArgs(tc.in); !slices.Equal(have, tc.want) {
				t.Fatalf("completionAliasArgs(%q) = %q but wanted %q", tc.in, have, tc.want)
			}
		})
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
		if status := cmd.Main([]string{"actionlint", "-completion", "auto"}); status != ExitStatusSuccessNoProblem {
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
		if status := cmd.Main([]string{"actionlint", "-completion", "auto"}); status != ExitStatusInvalidCommandOption {
			t.Fatalf("exit status is %d. stdout:\n%s", status, stdout)
		}
		if !strings.Contains(stderr.String(), "cannot detect the current shell") {
			t.Errorf("stderr does not explain the detection failure:\n%s", stderr)
		}
	})
}

func TestCompletionZshSpecForms(t *testing.T) {
	script := testCompletionScript(t, completionShellZsh)
	for _, spec := range []string{
		`'(--completion)-completion=[Print a shell completion script for the given shell]:shell:(bash fish powershell zsh)'`,
		`'(-completion)--completion=[Print a shell completion script for the given shell]:shell:(bash fish powershell zsh)'`,
		`'*-ignore=[Regular expression matching to error messages you want to ignore]:value:'`,
		`'*--ignore=[Regular expression matching to error messages you want to ignore]:value:'`,
		`'(--oneline)-oneline[Use one line per one error]'`,
		`'(-oneline)--oneline[Use one line per one error]'`,
		`'(--h)-h[Show this help message]'`,
	} {
		if !strings.Contains(script, spec) {
			t.Errorf("zsh script lacks the spec %s:\n%s", spec, script)
		}
	}
}

func testCompletionPlayground(t *testing.T, withGlobFile bool) string {
	t.Helper()
	dir := t.TempDir()
	files := []string{"a.yaml", "b.yml", "c.txt", "my file.yaml", "qa.yaml", "qb.yaml"}
	if withGlobFile {
		files = append(files, "q*.yaml")
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "d.yaml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testCompletionSortedSet(t *testing.T, line string) []string {
	t.Helper()
	var set []string
	for s := range strings.SplitSeq(line, "|") {
		if s != "" {
			set = append(set, s)
		}
	}
	slices.Sort(set)
	return set
}

const testCompletionBashDriver = `source "$SCRIPT"
run() {
  COMP_WORDS=("$@")
  COMP_CWORD=$(( ${#COMP_WORDS[@]} - 1 ))
  COMPREPLY=()
  _actionlint
  local IFS='|'
  printf '%s\n' "${COMPREPLY[*]}"
}
cd "$PLAY" || exit 1
run actionlint -co
run actionlint --co
run actionlint -completion z
run actionlint -completion = z
run actionlint -completion =
run actionlint -config-file ''
run actionlint q
run actionlint my
run actionlint -he
`

func TestCompletionBashBehaviour(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the driver script needs POSIX paths")
	}
	bin, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	want := [][]string{
		{"-color", "-completion", "-config-file"},
		{"--color", "--completion", "--config-file"},
		{"zsh"},
		{"zsh"},
		{"bash", "fish", "powershell", "zsh"},
		{"a.yaml", "b.yml", "my file.yaml", "q*.yaml", "qa.yaml", "qb.yaml", "sub"},
		{"q*.yaml", "qa.yaml", "qb.yaml"},
		{"my file.yaml"},
		{"-help"},
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "actionlint.bash")
	if err := os.WriteFile(script, []byte(testCompletionScript(t, completionShellBash)), 0o600); err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join(dir, "drive.sh")
	if err := os.WriteFile(driver, []byte(testCompletionBashDriver), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), bin, "--norc", "--noprofile", driver)
	cmd.Env = append(os.Environ(), "SCRIPT="+script, "PLAY="+testCompletionPlayground(t, true))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driving the bash completion failed: %v\n%s", err, out)
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("driver printed %d lines but %d runs were expected:\n%s", len(lines), len(want), out)
	}
	for i, w := range want {
		slices.Sort(w)
		if have := testCompletionSortedSet(t, lines[i]); !slices.Equal(have, w) {
			t.Errorf("run %d completed to %q but wanted %q", i, have, w)
		}
	}
}

func testCompletionLastToken(line string) string {
	start := 0
	for i := range len(line) {
		if line[i] == ' ' && (i == 0 || line[i-1] != '\\') {
			start = i + 1
		}
	}
	return strings.ReplaceAll(line[start:], `\ `, " ")
}

func TestCompletionFishBehaviour(t *testing.T) {
	bin, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish is not installed")
	}

	tests := []struct {
		line string
		want []string
	}{
		{"actionlint -co", []string{"-color", "-completion", "-config-file"}},
		{"actionlint --co", []string{"--color", "--completion", "--config-file"}},
		{"actionlint -completion z", []string{"zsh"}},
		{"actionlint --completion=z", []string{"--completion=zsh"}},
		{"actionlint -config-file ", []string{"a.yaml", "b.yml", "my file.yaml", "q*.yaml", "qa.yaml", "qb.yaml", "sub/"}},
		{`actionlint -config-file my\ fi`, []string{"my file.yaml"}},
		{"actionlint sub/", []string{"sub/d.yaml"}},
		{"actionlint -stdin-filename ", []string{"a.yaml", "b.yml", "c.txt", "my file.yaml", "q*.yaml", "qa.yaml", "qb.yaml", "sub/"}},
		{"actionlint -he", []string{"-help"}},
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "actionlint.fish")
	if err := os.WriteFile(script, []byte(testCompletionScript(t, completionShellFish)), 0o600); err != nil {
		t.Fatal(err)
	}
	play := testCompletionPlayground(t, true)
	// An actionlint executable on $PATH would make fish derive extra candidates from its --help
	// output, so the driver runs with a $PATH holding no commands at all.
	emptyDir := filepath.Join(dir, "empty")
	if err := os.Mkdir(emptyDir, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), bin, "--no-config", "-c", fmt.Sprintf("source '%s'; complete -C '%s'", script, tc.line))
			cmd.Dir = play
			cmd.Env = append(os.Environ(), "PATH="+emptyDir)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("driving the fish completion failed: %v\n%s", err, out)
			}

			// fish 4.0 through 4.3.2 add subsequence matches and long options to the
			// `complete -C` output for a short-option token (fish-shell 85e76ba3561).
			token := testCompletionLastToken(tc.line)
			var have []string
			for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
				if cand, _, _ := strings.Cut(line, "\t"); cand != "" && strings.HasPrefix(cand, token) {
					have = append(have, cand)
				}
			}
			slices.Sort(have)
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(have, want) {
				t.Errorf("%q completed to %q but wanted %q", tc.line, have, want)
			}
		})
	}
}

const testCompletionPwshDriver = `param($Script, $Play, $LinesFile)
. $Script
Set-Location $Play
foreach ($Line in (Get-Content $LinesFile)) {
    $r = TabExpansion2 -inputScript $Line -cursorColumn $Line.Length
    $texts = @($r.CompletionMatches | ForEach-Object { $_.CompletionText })
    Write-Output ($texts -join '|')
}
`

func TestCompletionPwshBehaviour(t *testing.T) {
	var bin string
	for _, c := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(c); err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		t.Skip("neither pwsh nor powershell is installed")
	}

	tests := []struct {
		line string
		want []string
	}{
		{"actionlint -co", []string{"-color", "-completion", "-config-file"}},
		{"actionlint --co", []string{"--color", "--completion", "--config-file"}},
		{"actionlint -completion z", []string{"zsh"}},
		{"actionlint -completion=z", []string{"-completion=zsh"}},
		{"actionlint --completion=z", []string{"--completion=zsh"}},
		{"actionlint -config-file sub/", []string{"sub/d.yaml"}},
		{"actionlint sub/d", []string{"sub/d.yaml"}},
		{"actionlint my", []string{"'my file.yaml'"}},
		{"actionlint -he", []string{"-help"}},
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "actionlint.ps1")
	if err := os.WriteFile(script, []byte(testCompletionScript(t, completionShellPowerShell)), 0o600); err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join(dir, "drive.ps1")
	if err := os.WriteFile(driver, []byte(testCompletionPwshDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	linesFile := filepath.Join(dir, "lines.txt")
	var lines strings.Builder
	for _, tc := range tests {
		lines.WriteString(tc.line + "\n")
	}
	if err := os.WriteFile(linesFile, []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), bin, "-NoProfile", "-NonInteractive", "-File", driver, script, testCompletionPlayground(t, false), linesFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driving the PowerShell completion failed: %v\n%s", err, out)
	}

	printed := strings.Split(strings.TrimRight(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n"), "\n")
	if len(printed) != len(tests) {
		t.Fatalf("driver printed %d lines but %d runs were expected:\n%s", len(printed), len(tests), out)
	}
	for i, tc := range tests {
		want := slices.Clone(tc.want)
		slices.Sort(want)
		if have := testCompletionSortedSet(t, printed[i]); !slices.Equal(have, want) {
			t.Errorf("%q completed to %q but wanted %q", tc.line, have, want)
		}
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
