package ruff

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDynamicStringContents(t *testing.T) {
	binary, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"percent", `value = "x"; print("${{ inputs.format }}" % value)`, nil},
		{"percent mapping", `print("${{ inputs.format }}" % {"key": "x"})`, nil},
		{"percent conversion", `print("% ${{ inputs.conversion }}" % 1)`, nil},
		{"percent adjacent conversion", `print("%${{ inputs.conversion }}" % 1)`, nil},
		{"percent width conversion", `print("%03${{ inputs.conversion }}" % 1)`, nil},
		{"static percent conversion retained", `print("%Q${{ inputs.suffix }}" % 1)`, []string{"F509"}},
		{"format positional", `print("${{ inputs.format }}".format("x"))`, nil},
		{"format named", `print("${{ inputs.format }}".format(key="x"))`, nil},
		{"format mixed", `print("prefix {} ${{ inputs.format }}".format("x", "y"))`, nil},
		{"format brace completion", `print("${{ inputs.escape }}{".format())`, nil},
		{"multiline format", "print(\"\"\"prefix\n${{ inputs.format }}\"\"\".format(\"x\"))", nil},
		{"docstring", `def f(): """${{ inputs.docstring }}"""`, nil},
		{"multiline docstring", "def f():\n    \"\"\"${{\n inputs.docstring\n}}\"\"\"", nil},
		{"dynamic first paragraph", "def f():\n    \"\"\"Return one.\n    ${{ inputs.summary }}\n    \"\"\"", nil},
		{"static first paragraph", "def f():\n    \"\"\"Return one\n\n    ${{ inputs.body }}\n    \"\"\"", []string{"D400", "D415"}},
		{"static percent retained", `print("${{ inputs.format }}" % "x"); print("static" % "x")`, []string{"F507"}},
		{"static format retained", `print("${{ inputs.format }}".format("x")); print("static".format("x"))`, []string{"F523"}},
		{"static docstring retained", "def f(): \"\"\"${{ inputs.docstring }}\"\"\"\ndef g(): \"\"\"Return one\"\"\"", []string{"D400", "D415"}},
		{"quoted annotation reference", `def f(value: '${{ inputs.type }}'): pass`, nil},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+ending, func(t *testing.T) {
				script := strings.ReplaceAll(tc.source+"\nprint(missing)\n", "\n", ending)
				var result error
				var diagnostics []Diagnostic
				checker := New(func(args []string, source string, callback func([]byte, error) error) {
					cmd := exec.CommandContext(t.Context(), binary, args...)
					cmd.Stdin = strings.NewReader(source)
					var stderr bytes.Buffer
					cmd.Stderr = &stderr
					output, err := cmd.Output()
					var exit *exec.ExitError
					if errors.As(err, &exit) && exit.ExitCode() == 1 {
						err = nil
					}
					if err != nil {
						t.Fatalf("Ruff failed: %v: %s", err, stderr.String())
					}
					result = callback(output, nil)
				}, func() error { return result }, reviewExpressionEnd)
				python := "python"
				if err := checker.Check(script, &python, "workflow:12", Config{Select: []string{"F", "D400", "D415"}}, func(d Diagnostic) { diagnostics = append(diagnostics, d) }); err != nil {
					t.Fatal(err)
				}
				if err := checker.Wait(); err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, diagnostic := range diagnostics {
					got = append(got, diagnostic.Code)
				}
				if want := append(slices.Clone(tc.want), "F821"); !slices.Equal(got, want) {
					t.Fatalf("codes %v, want %v: %+v", got, want, diagnostics)
				}
				last := diagnostics[len(diagnostics)-1]
				if last.Location != (Position{Row: strings.Count(script, "\n"), Column: 7}) || !strings.Contains(last.Message, "missing") {
					t.Fatalf("unrelated finding moved: %+v", last)
				}
			})
		}
	}
}

func reviewExpressionEnd(source string) (int, bool) {
	end := strings.Index(source, "}}")
	return end + 2, end >= 0
}

func TestDynamicDocstringAllRules(t *testing.T) {
	binary, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	for _, tc := range []struct {
		name, source string
		present      []string
	}{
		{"dynamic summary", "def f():\n    \"\"\"${{ inputs.docstring }}\"\"\"\n    return 1\n", []string{"D100", "ANN201"}},
		{"static layout", "def f():\n    \"\"\"Static summary.\n\tStatic second line.\n    ${{ inputs.body }}\n    \"\"\"\n    return 1\n", []string{"D205", "D206"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result error
			var codes []string
			checker := New(func(args []string, source string, callback func([]byte, error) error) {
				cmd := exec.CommandContext(t.Context(), binary, args...)
				cmd.Stdin = strings.NewReader(source)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				output, err := cmd.Output()
				var exit *exec.ExitError
				if errors.As(err, &exit) && exit.ExitCode() == 1 {
					err = nil
				}
				if err != nil {
					t.Fatalf("Ruff failed: %v: %s", err, stderr.String())
				}
				result = callback(output, nil)
			}, func() error { return result }, reviewExpressionEnd)
			python := "python"
			if err := checker.Check(tc.source, &python, "workflow:12", Config{Select: []string{"ALL"}}, func(d Diagnostic) { codes = append(codes, d.Code) }); err != nil {
				t.Fatal(err)
			}
			if err := checker.Wait(); err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.present {
				if !slices.Contains(codes, want) {
					t.Errorf("static finding %s lost: %v", want, codes)
				}
			}
			if slices.Contains(codes, "D400") || slices.Contains(codes, "D415") {
				t.Errorf("synthetic docstring punctuation survived ALL: %v", codes)
			}
		})
	}
}

func TestAsyncSyntaxTemplateSkipped(t *testing.T) {
	for _, source := range []string{
		"async ${{ inputs.kind }} resource:\n    pass",
		"async ${{ inputs.kind }} item in values:\n    pass",
		"async ${{ inputs.kind }} f():\n    pass",
		"async \\\n    ${{ inputs.kind }} resource:\n    pass",
		"[item async ${{ inputs.kind }} item in values]",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(source+ending, func(t *testing.T) {
				checker := New(func([]string, string, func([]byte, error) error) {
					t.Fatal("syntax-bearing async template reached Ruff")
				}, func() error { return nil }, reviewExpressionEnd)
				python := "python"
				if err := checker.Check(strings.ReplaceAll(source, "\n", ending), &python, "workflow:12", Config{}, func(Diagnostic) {}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestAsyncValueTemplateRetained(t *testing.T) {
	for _, source := range []string{
		"async with ${{ inputs.resource }}:\n    pass",
		"async for item in ${{ inputs.values }}:\n    pass",
		"async \\\n    with ${{ inputs.resource }}:\n    pass",
		"async \\\n    for item in ${{ inputs.values }}:\n    pass",
	} {
		t.Run(source, func(t *testing.T) {
			calls := 0
			checker := New(func(_ []string, _ string, callback func([]byte, error) error) {
				calls++
				if err := callback([]byte("[]"), nil); err != nil {
					t.Fatal(err)
				}
			}, func() error { return nil }, reviewExpressionEnd)
			python := "python"
			if err := checker.Check(source, &python, "workflow:12", Config{}, func(Diagnostic) {}); err != nil || calls != 1 {
				t.Fatalf("value-bearing async template skipped: calls=%d, error=%v", calls, err)
			}
		})
	}
}

func TestIsolatedConfigArguments(t *testing.T) {
	directory := t.TempDir()
	fileWithEquals := filepath.Join(directory, "config=custom.toml")
	if err := os.WriteFile(fileWithEquals, []byte("line-length = 88\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value string
		inline      bool
	}{
		{"relative file", "ruff.toml", false},
		{"absolute file", filepath.Join(directory, "ruff.toml"), false},
		{"equals in existing file", "config=custom.toml", false},
		{"quoted equals file", `"some=name.toml"`, false},
		{"value", "line-length = 100", true},
		{"nested key", "lint.extend-select = ['B']", true},
		{"quoted key", `"lint"."per-file-ignores" = {"a=b.py" = ["F401"]}`, true},
		{"comment", `ruff.toml # not = inline`, false},
		{"empty", "", false},
	} {
		for _, attached := range []bool{false, true} {
			flags := []string{"--config", tc.value}
			if attached {
				flags = []string{"--config=" + tc.value}
			}
			t.Run(tc.name+strings.Join(flags, " "), func(t *testing.T) {
				calls := 0
				checker := New(func(args []string, _ string, callback func([]byte, error) error) {
					calls++
					if !slices.Equal(args[1:1+len(flags)], flags) || !slices.Contains(args, "--isolated") {
						t.Fatalf("inline config not preserved: %v", args)
					}
					if err := callback([]byte("[]"), nil); err != nil {
						t.Fatal(err)
					}
				}, func() error { return nil }, nil, flags...)
				checker.WorkingDirectory(directory)
				python := "python"
				err := checker.Check("print(1)", &python, "workflow:12", Config{}, func(Diagnostic) {})
				if tc.inline {
					if err != nil || calls != 1 {
						t.Fatalf("inline config rejected: calls=%d, error=%v", calls, err)
					}
				} else if err == nil || calls != 0 || !strings.Contains(err.Error(), "configuration files cannot be used in isolated mode") && !strings.Contains(err.Error(), "requires a value") {
					t.Fatalf("config file reached Ruff: calls=%d, error=%v", calls, err)
				}
			})
		}
	}
}
