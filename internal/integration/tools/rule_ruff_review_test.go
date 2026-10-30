package tools_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func ruffFindingsWithInterpolation(t *testing.T, findings []diagnostic.Diagnostic, interpolationLines ...int) []diagnostic.Diagnostic {
	t.Helper()
	var ruff []diagnostic.Diagnostic
	wantLines := slices.Clone(interpolationLines)
	for _, finding := range findings {
		switch finding.Rule {
		case "ruff":
			ruff = append(ruff, finding)
		case "script-interpolation":
			line := slices.Index(wantLines, finding.Start.Line)
			if finding.Code != "unknown-script-value" || line < 0 {
				t.Fatalf("unexpected interpolation finding: %+v", finding)
			}
			wantLines = slices.Delete(wantLines, line, line+1)
		default:
			t.Fatalf("unexpected independent finding: %+v", finding)
		}
	}
	if len(wantLines) != 0 {
		t.Fatalf("missing interpolation findings at lines %v", wantLines)
	}
	return ruff
}

func templateDiagnosticLines(script string, firstLine int) []int {
	var lines []int
	for offset := 0; offset < len(script); {
		relative := strings.Index(script[offset:], "${{")
		if relative < 0 {
			break
		}
		body := offset + relative + len("${{")
		for body < len(script) && strings.ContainsRune(" \t\r\n", rune(script[body])) {
			body++
		}
		lines = append(lines, firstLine+strings.Count(script[:body], "\n"))
		offset = body
	}
	return lines
}

func TestRuffPatternCaptureSkipsOnlyItsScript(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	for _, pattern := range []string{`{"x": x, **${{ 'rest' }}}`, `[*${{ 'rest' }}]`, `${{ 'Widget' }}()`, `Outer(${{ 'Widget' }}())`} {
		source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          match {}:
            case ` + pattern + `: pass
      - shell: python
        run: print(missing)
`
		result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Command: command}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
		if err != nil {
			t.Fatal(err)
		}
		ruff := ruffFindingsWithInterpolation(t, result.Diagnostics, templateDiagnosticLines(pattern, 9)...)
		if len(ruff) != 1 || ruff[0].Code != "F821" || ruff[0].Start.Line != 11 {
			t.Fatalf("capture template changed independent diagnostics: %+v", result.Diagnostics)
		}
	}
}

func TestRuffStatisticsRejected(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
	_, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Process: &processconfig.Options{Executable: &command, Arguments: []string{"--statistics"}}}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
	if err == nil || !strings.Contains(err.Error(), "statistics output is not supported") {
		t.Fatalf("statistics mode not rejected before invocation: %v", err)
	}
}

func TestRuffFromTemplatePreservesFindings(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	for _, script := range []string{
		"def generate():\n    yield from ${{ '[]' }}",
		"def generate():\n    raise (yield from ${{ '[]' }}) from ${{ 'None' }}",
		"raise ValueError() from ${{ 'None' }}",
	} {
		for _, tc := range []struct{ suffix, code string }{
			{"print(missing)", "F821"}, {"if True print(1)", "invalid-syntax"},
		} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(script+tc.code+ending, func(t *testing.T) {
					source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          ` + strings.ReplaceAll(script+"\n"+tc.suffix, "\n", "\n          ") + "\n"
					source = strings.ReplaceAll(source, "\n", ending)
					result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Command: command}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
					if err != nil {
						t.Fatal(err)
					}
					ruff := ruffFindingsWithInterpolation(t, result.Diagnostics, templateDiagnosticLines(script, 8)...)
					if len(ruff) == 0 {
						t.Fatal("from clause caused script checking to be skipped")
					}
					for _, finding := range ruff {
						if finding.Rule != "ruff" || finding.Code != tc.code || finding.Start.Line != 9+strings.Count(script, "\n") {
							t.Fatalf("from clause changed independent findings: %+v", result.Diagnostics)
						}
					}
				})
			}
		}
	}
}

func TestRuffSilentRejected(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	for _, flag := range []string{"--silent", "-s", "-qs"} {
		source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
		_, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Process: &processconfig.Options{Executable: &command, Arguments: []string{flag}}}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
		if err == nil || !strings.Contains(err.Error(), "silent output is not supported") {
			t.Fatalf("silent mode not rejected before invocation: %v", err)
		}
	}
}

func TestRuffOperatorTemplateSkipsOnlyItsScript(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          if lhs ${{ '==' }} rhs: pass
      - shell: python
        run: print(missing)
`
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Command: command}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
	if err != nil {
		t.Fatal(err)
	}
	ruff := ruffFindingsWithInterpolation(t, result.Diagnostics, templateDiagnosticLines(source, 1)...)
	if len(ruff) != 1 || ruff[0].Code != "F821" || ruff[0].Start.Line != 10 {
		t.Fatalf("operator template changed independent diagnostics: %+v", result.Diagnostics)
	}
}

func TestRuffVersionedShellGrammar(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	for _, tc := range []struct {
		config     string
		wantSyntax bool
	}{
		{"", true}, {"tools: {ruff: {target-version: py314}}", false},
	} {
		config, err := repoconfig.Parse([]byte(tc.config))
		if err != nil {
			t.Fatal(err)
		}
		source := `on: push
defaults:
  run:
    shell: python3.9 {0}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: |
          match 1:
            case 1: pass
`
		result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Command: command}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source), Config: config}})
		if err != nil {
			t.Fatal(err)
		}
		hasSyntax := false
		for _, finding := range result.Diagnostics {
			hasSyntax = hasSyntax || finding.Code == "invalid-syntax"
		}
		if hasSyntax != tc.wantSyntax {
			t.Fatalf("syntax=%v, want %v; findings=%+v", hasSyntax, tc.wantSyntax, result.Diagnostics)
		}
	}
}

func TestRuffOutputRedirectionDoesNotWrite(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	for _, option := range []string{"--output-file", "--output-file=", "-o", "-o=", "-oattached", "-qoattached"} {
		t.Run(option, func(t *testing.T) {
			root := t.TempDir()
			output := testutil.WriteFixture(t, root, "output.json", "preserve this file")
			flags := []string{option, output}
			if strings.HasSuffix(option, "=") {
				flags = []string{option + output}
			} else if before, ok := strings.CutSuffix(option, "attached"); ok {
				flags = []string{before + output}
			}
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
			_, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"ruff": {Process: &processconfig.Options{Executable: &command, Arguments: flags}}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
			if err == nil || !strings.Contains(err.Error(), "output redirection") {
				t.Errorf("redirection not rejected: %v", err)
			}
			if content, err := os.ReadFile(output); err != nil || string(content) != "preserve this file" {
				t.Fatalf("explicit output file modified: %q, %v", content, err)
			}
		})
	}
}
