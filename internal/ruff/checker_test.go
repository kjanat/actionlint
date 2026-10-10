package ruff

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestArguments(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		config                      Config
		selectRules, ignore, target string
	}{
		{"defaults", Config{}, "F", "", "py314"},
		{"selection", Config{Select: []string{"F", "B"}, Ignore: []string{"F401"}, TargetVersion: "py312"}, "F,B", "F401", "py312"},
		{"empty selection", Config{Select: []string{}}, "ALL", "ALL", "py314"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := arguments(tc.config, "actionlint.py")
			for flag, want := range map[string]string{"--select": tc.selectRules, "--ignore": tc.ignore, "--target-version": tc.target} {
				i := slices.Index(args, flag)
				if want == "" {
					if i != -1 {
						t.Fatalf("unexpected %s", flag)
					}
					continue
				}
				if i < 0 || i+1 >= len(args) || args[i+1] != want {
					t.Fatalf("%s: %v", flag, args)
				}
			}
			for _, flag := range []string{"--isolated", "--ignore-noqa", "--no-fix", "--no-cache"} {
				if !slices.Contains(args, flag) {
					t.Fatalf("missing %s", flag)
				}
			}
			if args[len(args)-1] != "-" {
				t.Fatal("not reading stdin")
			}
		})
	}
}

func TestCheckDiagnosticFilename(t *testing.T) {
	foreignDirectory := t.TempDir()
	for _, tc := range []struct {
		name      string
		filename  func(string) string
		wantError bool
	}{
		{"stdin", func(path string) string { return path }, false},
		{"normalized stdin", func(path string) string {
			return filepath.Dir(path) + string(filepath.Separator) + "nested" + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(path)
		}, false},
		{"different file", func(path string) string { return filepath.Join(filepath.Dir(path), "other.py") }, true},
		{"same basename elsewhere", func(path string) string { return filepath.Join(filepath.Dir(path), "other", filepath.Base(path)) }, true},
		{"existing foreign directory", func(string) string { return filepath.Join(foreignDirectory, "actionlint.py") }, true},
		{"missing filename", func(string) string { return "" }, true},
		{"relative filename", func(string) string { return "actionlint.py" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result error
			var reported int
			checker := New(func(args []string, _ string, callback func([]byte, error) error) {
				filename := args[slices.Index(args, "--stdin-filename")+1]
				filename, err := filepath.Abs(filename)
				if err != nil {
					t.Fatal(err)
				}
				diagnostic := Diagnostic{Filename: filename, Code: "F821", Message: "undefined", Location: Position{Row: 1, Column: 1}}
				diagnostics := []Diagnostic{diagnostic}
				diagnostic.Filename = tc.filename(filename)
				diagnostics = append(diagnostics, diagnostic)
				output, err := json.Marshal(diagnostics)
				if err != nil {
					t.Fatal(err)
				}
				result = callback(output, nil)
			}, func() error { return result }, nil)
			python := "python"
			if err := checker.Check("print(missing)", &python, "test", Config{}, func(Diagnostic) { reported++ }); err != nil {
				t.Fatal(err)
			}
			err := checker.Wait()
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError = %v", err, tc.wantError)
			}
			if tc.wantError && reported != 0 {
				t.Fatalf("reported %d diagnostics before rejecting foreign input", reported)
			}
			if !tc.wantError && reported != 2 {
				t.Fatalf("reported %d diagnostics, want 2", reported)
			}
		})
	}
}

func TestCheckDiagnosticDirectoryAlias(t *testing.T) {
	directory := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	var result error
	checker := New(func(_ []string, _ string, callback func([]byte, error) error) {
		diagnostic := Diagnostic{Filename: filepath.Join(alias, "actionlint.py"), Code: "F821", Message: "undefined", Location: Position{Row: 1, Column: 1}}
		output, err := json.Marshal([]Diagnostic{diagnostic})
		if err != nil {
			t.Fatal(err)
		}
		result = callback(output, nil)
	}, func() error { return result }, nil)
	checker.WorkingDirectory(directory)
	python := "python"
	reported := 0
	if err := checker.Check("print(missing)", &python, "test", Config{}, func(Diagnostic) { reported++ }); err != nil {
		t.Fatal(err)
	}
	if err := checker.Wait(); err != nil || reported != 1 {
		t.Fatalf("physical directory alias was rejected: reported=%d, error=%v", reported, err)
	}
}

func TestCheckStdinIgnoresPositionalInputs(t *testing.T) {
	binary, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	directory := t.TempDir()
	external := filepath.Join(directory, "external.py")
	if err := os.WriteFile(external, []byte("print(external_file_only)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{external, directory} {
		t.Run(filepath.Base(input), func(t *testing.T) {
			var result error
			var diagnostics []Diagnostic
			checker := New(func(args []string, source string, callback func([]byte, error) error) {
				cmd := exec.CommandContext(t.Context(), binary, args...)
				cmd.Stdin = strings.NewReader(source)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				output, runErr := cmd.Output()
				var exitErr *exec.ExitError
				if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
					runErr = nil
				}
				if !strings.Contains(stderr.String(), "in favor of standard input") {
					t.Fatalf("missing Ruff input warning: %s", stderr.String())
				}
				result = callback(output, runErr)
			}, func() error { return result }, nil, input)
			python := "python"
			if err := checker.Check("print(workflow_only)", &python, "test", Config{}, func(d Diagnostic) { diagnostics = append(diagnostics, d) }); err != nil {
				t.Fatal(err)
			}
			if err := checker.Wait(); err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "workflow_only") {
				t.Fatalf("unexpected stdin diagnostics: %+v", diagnostics)
			}
		})
	}
}

func TestShellSelectionAndFork(t *testing.T) {
	var calls int
	c := New(func(_ []string, _ string, callback func([]byte, error) error) {
		calls++
		if err := callback([]byte("[]"), nil); err != nil {
			t.Fatal(err)
		}
	}, func() error { return nil }, nil)
	check := func(c *Checker, shell *string, config Config, want int) {
		t.Helper()
		if err := c.Check("print(1)", shell, "test", config, func(Diagnostic) { t.Fatal("unexpected diagnostic") }); err != nil {
			t.Fatal(err)
		}
		if calls != want {
			t.Fatalf("got %d calls, want %d", calls, want)
		}
	}
	python, bash, dynamic := "python3 -u {0}", "bash", "${{ inputs.shell }}"
	check(c, nil, Config{}, 0)
	c.WorkflowShell(&python)
	check(c, nil, Config{}, 1)
	c.JobShell(&bash)
	check(c, nil, Config{}, 1)
	check(c, &python, Config{}, 2)
	check(c, &dynamic, Config{}, 2)
	c.JobShell(nil)
	check(c, nil, Config{}, 3)
	check(c, nil, Config{Enabled: new(false)}, 3)
	check(c.Fork(), nil, Config{}, 3)
	check(c.Fork(), &python, Config{}, 4)
}

func TestConfiguredFlagsFollowCheck(t *testing.T) {
	flags := []string{"--preview", "--exclude", "vendor/**"}
	calls := 0
	c := New(func(args []string, _ string, callback func([]byte, error) error) {
		calls++
		if !slices.Equal(args[:4], []string{"check", "--preview", "--exclude", "vendor/**"}) || args[4] != "--isolated" {
			t.Fatalf("configured flags outside check subcommand: %v", args)
		}
		if err := callback([]byte("[]"), nil); err != nil {
			t.Fatal(err)
		}
	}, func() error { return nil }, nil, flags...)
	flags[0] = "--mutated"
	python := "python"
	for _, checker := range []*Checker{c, c.Fork()} {
		if err := checker.Check("print(1)", &python, "test", Config{}, func(Diagnostic) { t.Fatal("unexpected diagnostic") }); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestStatisticsFlagRejected(t *testing.T) {
	checker := New(func([]string, string, func([]byte, error) error) {
		t.Fatal("statistics mode reached Ruff")
	}, func() error { return nil }, nil, "--statistics")
	python := "python"
	if err := checker.Check("print(missing)", &python, "test", Config{}, func(Diagnostic) {}); err == nil || !strings.Contains(err.Error(), "statistics output is not supported") {
		t.Fatalf("statistics mode not rejected: %v", err)
	}
}

func TestOutputRedirectionFlagsRejected(t *testing.T) {
	for _, flags := range [][]string{
		{"--output-file", "out.json"}, {"--output-file=out.json"},
		{"-o", "out.json"}, {"-oout.json"}, {"-o=out.json"}, {"-qoout.json"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			checker := New(func([]string, string, func([]byte, error) error) {
				t.Error("scheduled Ruff with output redirection")
			}, func() error { return nil }, nil, flags...)
			python := "python"
			if err := checker.Check("print(missing)", &python, "test", Config{}, func(Diagnostic) {}); err == nil || !strings.Contains(err.Error(), "output redirection") {
				t.Fatalf("output redirection was not rejected: %v", err)
			}
		})
	}
}

func TestVersionedPythonShells(t *testing.T) {
	for _, tc := range []struct {
		shell string
		want  bool
	}{
		{"python", true},
		{"python3 -u {0}", true},
		{"python3.12 {0}", true},
		{"python3.9 -u {0}", true},
		{"/usr/local/bin/python3.13 {0}", true},
		{`C:\Python\python3.12.exe {0}`, true},
		{"python.exe {0}", true},
		{"py {0}", true},
		{"py -3 -u {0}", true},
		{"py.exe {0}", true},
		{`C:\Windows\py.exe {0}`, true},
		{`C:\Windows\PY.EXE {0}`, true},
		{"/usr/local/bin/py {0}", true},
		{"", false},
		{"pypy {0}", false},
		{"py-helper {0}", false},
		{"py3 {0}", false},
		{`C:\py\bash.exe {0}`, false},
		{"python2.7 {0}", false},
		{"python3. {0}", false},
		{"python3.12-config {0}", false},
		{"python3.12x {0}", false},
		{"/python3.12/bash {0}", false},
		{"${{ inputs.shell }} {0}", false},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			called := false
			checker := New(func([]string, string, func([]byte, error) error) {
				called = true
			}, func() error { return nil }, nil)
			if err := checker.Check("print(1)", &tc.shell, "test", Config{}, func(Diagnostic) {}); err != nil {
				t.Fatal(err)
			}
			if called != tc.want {
				t.Fatalf("checker called=%v, want %v", called, tc.want)
			}
		})
	}
}

func TestActionsShellPythonModes(t *testing.T) {
	for _, tc := range []struct {
		shell string
		want  bool
	}{
		{"actions-shell python {0}", true},
		{"actions-shell py {0}", true},
		{"actions-shells python {0}", true},
		{"actions-shells py -u {0}", true},
		{"actions-shell.cmd python {0}", true},
		{"actions-shells.cmd py {0}", true},
		{"/usr/local/bin/actions-shell python {0}", true},
		{`C:\tools\actions-shells.cmd py {0}`, true},
		{`actions-shell "python" -W "ignore::DeprecationWarning" "{0}"`, true},
		{"\tactions-shell\t'py' -- '{0}'", true},
		{"actions-shell rust {0}", false},
		{"actions-shells node {0}", false},
		{"actions-shell python3 {0}", false},
		{"actions-shell describe python {0}", false},
		{"actions-shell -- python {0}", false},
		{"actions-shell python", false},
		{"actions-shell python {0} unrelated.py", false},
		{"other-wrapper python {0}", false},
		{"actions-shell-helper python {0}", false},
		{"actions-shell $RUNTIME {0}", false},
		{"actions-shell `echo python` {0}", false},
		{"actions-shell python {0}; echo ignored", false},
		{"actions-shell 'python {0}", false},
		{"actions-shell ${{ inputs.runtime }} {0}", false},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			called := false
			checker := New(func([]string, string, func([]byte, error) error) {
				called = true
			}, func() error { return nil }, nil)
			if err := checker.Check("print(missing)", &tc.shell, "test", Config{}, func(Diagnostic) {}); err != nil {
				t.Fatal(err)
			}
			if called != tc.want {
				t.Fatalf("checker called=%v, want %v", called, tc.want)
			}
		})
	}
}

func TestCheckFailureAndAtomicDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		output     string
		processErr error
	}{
		{"[]", errors.New("exit 2")},
		{"null", nil},
		{`[{"code":"F821","message":"missing","location":{"row":1,"column":1}},{}]`, nil},
	} {
		var result error
		c := New(func(_ []string, _ string, callback func([]byte, error) error) {
			result = callback([]byte(tc.output), tc.processErr)
		}, func() error { return result }, nil)
		python := "python"
		if err := c.Check("print(1)", &python, "workflow:12", Config{}, func(Diagnostic) { t.Fatal("partially emitted invalid response") }); err != nil {
			t.Fatal(err)
		}
		if err := c.Wait(); err == nil || !strings.Contains(err.Error(), "workflow:12") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestSanitize(t *testing.T) {
	source := "print(${{\n 'é'\n}}, missing)"
	got, valid, err := Sanitize(source, func(string) (int, bool) { return len("\n 'é'\n}}"), true })
	if err != nil || !valid || len([]rune(got)) != len([]rune(source)) || strings.Contains(got, "${{") {
		t.Fatalf("%q %v", got, valid)
	}
	for i, r := range []rune(source) {
		if r == '\n' && []rune(got)[i] != r {
			t.Fatal("line break moved")
		}
	}
	for _, end := range []int{-1, 0, 1, len(source) + 1} {
		if _, valid, _ := Sanitize(source, func(string) (int, bool) { return end, true }); valid {
			t.Fatalf("accepted end %d", end)
		}
	}
}

func TestSanitizeTemplateContexts(t *testing.T) {
	end := func(source string) (int, bool) {
		index := strings.Index(source, "}}")
		return index + 2, index >= 0
	}
	for _, source := range []string{
		"# ${{\n\n value\n}} trailing\nprint(missing)",
		"print(${{\n\n value\n}})\nprint(missing)",
		"print('''${{\n\n value\n}}''')\nprint(missing)",
		"print(\"# ${{\n value\n}}\")\nprint(missing)",
		"print('${{ value }}') # ${{\n value\n}}\nprint(missing)",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			t.Run(source, func(t *testing.T) {
				got, valid, err := Sanitize(source, end)
				if err != nil || !valid || len([]rune(got)) != len([]rune(source)) {
					t.Fatalf("got %q, valid %v, error %v", got, valid, err)
				}
				for i, r := range []rune(source) {
					if (r == '\n' || r == '\r') && []rune(got)[i] != r {
						t.Fatal("line ending moved")
					}
				}
				if !strings.HasSuffix(got, "print(missing)") {
					t.Fatal("unrelated code changed")
				}
			})
		}
	}
}

func TestQuotedEmptyTemplateLineSkipsScript(t *testing.T) {
	for _, quote := range []string{"'", "\""} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(quote+ending, func(t *testing.T) {
				c := New(func([]string, string, func([]byte, error) error) {
					t.Fatal("sent an invalid sanitized script to Ruff")
				}, func() error { return nil }, func(source string) (int, bool) {
					return strings.Index(source, "}}") + 2, true
				})
				python := "python"
				script := strings.ReplaceAll("print("+quote+"${{\n\n value\n}}"+quote+")\nprint(missing)", "\n", ending)
				err := c.Check(script, &python, "workflow:12", Config{}, func(Diagnostic) { t.Fatal("unexpected diagnostic") })
				if err != nil {
					t.Fatalf("unsupported interpolation aborted analysis: %v", err)
				}
			})
		}
	}
}

func TestSanitizeAdjacentPythonTokens(t *testing.T) {
	end := func(source string) (int, bool) {
		index := strings.Index(source, "}}")
		return index + 2, index >= 0
	}
	for _, source := range []string{
		"value = ${{ major }}.0",
		"value = 3.${{ minor }}",
		"value = 0x${{ hex }}",
		"value = ${{ number }}e2",
		"item_${{ os }} = 1",
		"${{ prefix }}_item = 1",
		"é${{ suffix }} = 1",
		"a\u0301${{ suffix }} = 1",
		"value = ${{ prefix }}'literal'",
		"value = ${{ first }}${{ second }}",
		"value = ${{\n major\n}}.0",
		"item_${{\n os\n}} = 1",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			t.Run(source, func(t *testing.T) {
				if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
					t.Fatalf("got %q, valid %v, error %v", got, valid, err)
				}
				checker := New(func([]string, string, func([]byte, error) error) {
					t.Fatal("dispatched a script with an interpolated token fragment")
				}, func() error { return nil }, end)
				python := "python"
				if err := checker.Check(source, &python, "workflow:12", Config{}, func(Diagnostic) {
					t.Fatal("reported a finding from a skipped script")
				}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestSanitizeAdjacentPythonDelimiters(t *testing.T) {
	end := func(source string) (int, bool) { return strings.Index(source, "}}") + 2, true }
	for _, source := range []string{
		"value=${{ number }}+1",
		"value=[${{ number }},2]",
		"value={${{ number }}:2}",
		"value=(${{ number }})",
		"value=${{\n number\n}}+1",
		"value='item_${{ name }}.0'",
		"value=\"item_${{ name }}.0\"",
		"# item_${{ name }}.0\nvalue=1",
	} {
		t.Run(source, func(t *testing.T) {
			got, valid, err := Sanitize(source, end)
			if err != nil || !valid || len([]rune(got)) != len([]rune(source)) {
				t.Fatalf("got %q, valid %v, error %v", got, valid, err)
			}
			for i, r := range []rune(source) {
				if (r == '\n' || r == '\r') && []rune(got)[i] != r {
					t.Fatal("line ending moved")
				}
			}
		})
	}
}
