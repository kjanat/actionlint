package ruff

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNonDiagnosticModesRejected(t *testing.T) {
	for _, flag := range []string{"--help", "-h", "-qh", "--watch", "-w", "-qw", "--add-noqa", "--add-noqa=reviewed", "--add-ignore", "--add-ignore=reviewed"} {
		t.Run(flag, func(t *testing.T) {
			checker := New(func([]string, string, func([]byte, error) error) { t.Fatal("non-diagnostic mode reached Ruff") }, func() error { return nil }, nil, flag)
			python := "python"
			if err := checker.Check("print(missing)", &python, "workflow:12", Config{}, func(Diagnostic) {}); err == nil || !strings.Contains(err.Error(), "non-diagnostic mode") {
				t.Fatalf("non-diagnostic mode not rejected: %v", err)
			}
		})
	}
}

func TestFStringConversionTemplates(t *testing.T) {
	end := func(source string) (int, bool) { index := strings.Index(source, "}}"); return index + 2, index >= 0 }
	for _, source := range []string{
		`print(f"{1:'>5} {2!${{ 'r' }}}")`,
		`print(f"{1:{2!${{ 'r' }}}}")`,
		`print(f"{ {'key': 1} !${{ 'r' }}}")`,
		`print(f"{(lambda: 1)()!${{ 'r' }}}")`,
		"print(f\"\"\"{1 # ' comment\n!${{ 'r' }}}\"\"\")",
		`print(f"{value!${{ inputs.conversion }}}")`,
		`print(F'{value!${{ inputs.conversion }}}')`,
		`print(rf"{value!${{ inputs.conversion }}}")`,
		`print(t"{value!${{ inputs.conversion }}}")`,
		`print(rt"{value!${{ inputs.conversion }}}")`,
		`print(tr"{value!${{ inputs.conversion }}}")`,
		`print(f"{"value"!${{ inputs.conversion }}}")`,
		`print(f"{r"value"!${{ inputs.conversion }}}")`,
		`print(f"""{"""value"""!${{ inputs.conversion }}}""")`,
		`print(f"{f"{value!${{ inputs.conversion }}}"}")`,
		"print(f\"\"\"{value!${{\n inputs.conversion\n}}}\"\"\")",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
				t.Fatalf("conversion template accepted: %q, %v, %v", got, valid, err)
			}
		}
	}
	for _, source := range []string{
		`print("!${{ inputs.value }}")`,
		`print(f"literal!${{ inputs.value }}")`,
		`print(f"{{literal!${{ inputs.value }}}}")`,
		`print(f"{value != ${{ inputs.value }}}")`,
		`print(f"{${{ inputs.value }}!r}")`,
		`print(f"{'!${{ inputs.value }}'}")`,
		`print(f"{"!${{ inputs.value }}"}")`,
		`# f"{value!${{ inputs.conversion }}}"`,
	} {
		if _, valid, err := Sanitize(source, end); err != nil || !valid {
			t.Fatalf("value template skipped: %q, %v, %v", source, valid, err)
		}
	}
}

func TestWorkingDirectorySurvivesFork(t *testing.T) {
	directory := t.TempDir()
	calls := 0
	checker := New(func(args []string, _ string, callback func([]byte, error) error) {
		calls++
		if index := slices.Index(args, "--stdin-filename"); index < 0 || args[index+1] != "actionlint.py" {
			t.Fatalf("stdin filename is not child-relative: %v", args)
		}
		output, err := json.Marshal([]Diagnostic{{Filename: filepath.Join(directory, "actionlint.py"), Code: "F821", Message: "undefined", Location: Position{Row: 1, Column: 1}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := callback(output, nil); err != nil {
			t.Fatal(err)
		}
	}, func() error { return nil }, nil)
	checker.WorkingDirectory(directory)
	python := "python"
	for _, c := range []*Checker{checker, checker.Fork()} {
		if err := c.Check("print(1)", &python, "test", Config{}, func(Diagnostic) {}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("got %d invocations, want 2", calls)
	}
}

func TestMissingWorkingDirectoryFailsBeforeScheduling(t *testing.T) {
	checker := New(func([]string, string, func([]byte, error) error) {
		t.Fatal("missing working directory reached Ruff")
	}, func() error { return nil }, nil)
	checker.WorkingDirectory(filepath.Join(t.TempDir(), "missing"))
	python := "python"
	if err := checker.Check("print(missing)", &python, "workflow:12", Config{}, func(Diagnostic) {}); err == nil || !strings.Contains(err.Error(), "ruff stdin directory for script at workflow:12") {
		t.Fatalf("missing working directory not reported: %v", err)
	}
}

func TestIntegrationFlagsRejected(t *testing.T) {
	for _, flag := range []string{"--isolated", "--ignore-noqa", "--no-fix", "--no-cache", "-n", "-qn", "--target-version", "--stdin-filename", "--output-format"} {
		for _, argument := range []string{flag, flag + "=value"} {
			t.Run(argument, func(t *testing.T) {
				checker := New(func([]string, string, func([]byte, error) error) {
					t.Fatal("integration-owned flag reached Ruff")
				}, func() error { return nil }, nil, argument)
				python := "python"
				if err := checker.Check("print(missing)", &python, "workflow:12", Config{}, func(Diagnostic) {}); err == nil || !strings.Contains(err.Error(), "integration-owned option") || !strings.Contains(err.Error(), "workflow:12") {
					t.Fatalf("integration flag not rejected: %v", err)
				}
			})
		}
	}
}

func TestSilentFlagsRejected(t *testing.T) {
	for _, flag := range []string{"--silent", "-s", "-qs"} {
		t.Run(flag, func(t *testing.T) {
			checker := New(func([]string, string, func([]byte, error) error) {
				t.Fatal("silent mode reached Ruff")
			}, func() error { return nil }, nil, flag)
			python := "python"
			if err := checker.Check("print(missing)", &python, "test", Config{}, func(Diagnostic) {}); err == nil || !strings.Contains(err.Error(), "silent output is not supported") {
				t.Fatalf("silent mode not rejected: %v", err)
			}
		})
	}
}

func TestVersionedShellTarget(t *testing.T) {
	for _, tc := range []struct{ shell, explicit, want string }{
		{"python3.9 {0}", "", "py39"},
		{"/usr/bin/python3.12 -u {0}", "", "py312"},
		{`C:\Python\PYTHON3.10.EXE {0}`, "", "py310"},
		{"python3.9 {0}", "py314", "py314"},
		{"python3.15 {0}", "", "py315"},
		{"python3.6 {0}", "", "py314"},
		{"python3.99 {0}", "", "py314"},
		{"python3 {0}", "", "py314"},
		{"py -3.9 {0}", "", "py39"},
		{"py -3.10 -u {0}", "", "py310"},
		{"py.exe -V:3.9 {0}", "", "py39"},
		{`C:\Windows\PY.EXE -V:3.10 -u {0}`, "", "py310"},
		{"/usr/local/bin/py -3.9-32 {0}", "", "py39"},
		{"py -V:3.10-64 {0}", "", "py310"},
		{"py -V:3.13-arm64 {0}", "", "py313"},
		{"py -V:PythonCore/3.9 {0}", "", "py39"},
		{"py -V:pYtHoNcOrE/3.10-64 {0}", "", "py310"},
		{`py "-3.9" {0}`, "", "py39"},
		{"py '-V:3.10' {0}", "", "py310"},
		{`py -3.9 "{0}"`, "", "py39"},
		{"py -3.10 '{0}'", "", "py310"},
		{"py -3.9 {0}", "py314", "py314"},
		{"py -3.10 {0}", "py39", "py39"},
		{"py -3 {0}", "", "py314"},
		{"py -V:3 {0}", "", "py314"},
		{"py -3.6 {0}", "", "py314"},
		{"py -3.99 {0}", "", "py314"},
		{"py -3.9-invalid {0}", "", "py314"},
		{"py -3.9-32-64 {0}", "", "py314"},
		{"py -V:OtherCompany/3.9 {0}", "", "py314"},
		{"py -PythonCore/3.9 {0}", "", "py314"},
		{"py -V:PythonCore/3 {0}", "", "py314"},
		{"py -V:PythonCore/ {0}", "", "py314"},
		{`py "-3.9 {0}`, "", "py314"},
		{"py -V:3.9.1 {0}", "", "py314"},
		{"py 3.9 {0}", "", "py314"},
		{"py # -3.9 {0}", "", "py314"},
		{"py -u -3.9 {0}", "", "py314"},
		{"py -W -3.9 {0}", "", "py314"},
		{"py {0} -3.9", "", "py314"},
		{"py -c 'print(\"-3.9\")' {0}", "", "py314"},
		{"py -3.9", "", "py314"},
		{"actions-shell python {0}", "", "py314"},
		{"actions-shells py -3.9 {0}", "", "py39"},
		{"actions-shell py -3.10 -u {0}", "", "py310"},
		{`C:\tools\ACTIONS-SHELLS.CMD py -V:PythonCore/3.9-64 {0}`, "", "py39"},
		{`/usr/bin/actions-shells 'py' '-V:pYtHoNcOrE/3.13-arm64' -W "ignore::DeprecationWarning" '{0}'`, "", "py313"},
		{" \tactions-shell\tpy\t-3.9\t{0} ", "", "py39"},
		{"actions-shells py -3.9 {0}", "py310", "py310"},
		{"actions-shells py -3.10 {0}", "py39", "py39"},
		{"actions-shells python -3.9 {0}", "", "py314"},
		{"actions-shells py -u -3.9 {0}", "", "py314"},
		{"actions-shells py -W '-3.9' {0}", "", "py314"},
		{"actions-shells py -V:OtherCompany/3.9 {0}", "", "py314"},
		{"actions-shells py -3.99 {0}", "", "py314"},
		{"actions-shells py -3 {0}", "", "py314"},
		{"actions-shells py -V:3.9.1 {0}", "", "py314"},
		{"actions-shells py -v:3.9 {0}", "", "py314"},
		{"actions-shells py '-3.9 unrelated' {0}", "", "py314"},
	} {
		t.Run(tc.shell+tc.explicit, func(t *testing.T) {
			calls := 0
			checker := New(func(args []string, _ string, _ func([]byte, error) error) {
				calls++
				if i := slices.Index(args, "--target-version"); i < 0 || args[i+1] != tc.want {
					t.Fatalf("target: %v; want %s", args, tc.want)
				}
			}, func() error { return nil }, nil)
			checker.WorkflowShell(&tc.shell)
			if err := checker.Check("print(1)", nil, "test", Config{TargetVersion: tc.explicit}, func(Diagnostic) {}); err != nil || calls != 1 {
				t.Fatalf("calls=%d, error=%v", calls, err)
			}
		})
	}
}

func TestAdapterTargetRequiresSupportedShell(t *testing.T) {
	for _, shell := range []string{
		"", "actions-shells py -3.9", "actions-shells py -3.9 {0} later.py",
		"actions-shells python3 -3.9 {0}", "actions-shells PY -3.9 {0}",
		"actions-shells -- py -3.9 {0}", "wrapper py -3.9 {0}",
		"actions-shells py -3.9 {0}; echo other", "actions-shells py '-3.9 {0}",
		"actions-shells $RUNTIME -3.9 {0}",
	} {
		t.Run(shell, func(t *testing.T) {
			if isPythonShell(shell) || pythonShellTarget(shell) != "" {
				t.Fatalf("unsupported adapter inferred a Python target: %q", shell)
			}
		})
	}
}

func TestSanitizeOperatorTemplates(t *testing.T) {
	end := func(source string) (int, bool) { index := strings.Index(source, "}}"); return index + 2, index >= 0 }
	for _, source := range []string{
		"if lhs ${{ inputs.operator }} rhs: pass",
		"value = lhs ${{ inputs.operator }} (rhs)",
		"value = left() ${{ inputs.operator }} -right",
		"value = 'left' ${{ inputs.operator }} 'right'",
		"value = values[0] ${{ inputs.operator }} other",
		"value = ${{ inputs.left }} ${{ inputs.operator }} other",
		"value = (left\n ${{ inputs.operator }}\n right)",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			t.Run(source, func(t *testing.T) {
				if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
					t.Fatalf("operator template accepted: %q, %v, %v", got, valid, err)
				}
			})
		}
	}
	for _, source := range []string{
		"if ${{ inputs.value }}: pass", "value = left + ${{ inputs.value }}",
		"if False: pass\nelif ${{ inputs.value }}: pass",
		"try: pass\nexcept ${{ inputs.error }}: pass", "raise ${{ inputs.error }}",
		"return ${{ inputs.value }}", "print(${{ inputs.value }})",
		"value = ${{ inputs.value }} + other", "value = ${{ inputs.value }} if test else other",
		"value = [${{ inputs.value }} for item in items]",
		"if left == ${{ inputs.value }} and right: pass",
		"value = 'lhs ${{ inputs.operator }} rhs'", "# lhs ${{ inputs.operator }} rhs",
	} {
		t.Run(source, func(t *testing.T) {
			if _, valid, err := Sanitize(source, end); err != nil || !valid {
				t.Fatalf("value template skipped: %v, %v", valid, err)
			}
		})
	}
}

func TestImportFromTemplatesRemainUnsupported(t *testing.T) {
	end := func(source string) (int, bool) { index := strings.Index(source, "}}"); return index + 2, index >= 0 }
	for _, source := range []string{
		"from ${{ inputs.module }} import name",
		"from module import ${{ inputs.name }}",
		"raise ValueError()\nfrom ${{ inputs.module }} import name",
		"raise ValueError(); from ${{ inputs.module }} import name",
		"def generate():\n    yield 1\n    from ${{ inputs.module }} import name",
		"def generate():\n    yield 1; from ${{ inputs.module }} import name",
		"if True: from ${{ inputs.module }} import name",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			t.Run(source, func(t *testing.T) {
				if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
					t.Fatalf("import template accepted: %q, %v, %v", got, valid, err)
				}
			})
		}
	}
}
