package actionlint

import (
	"os"
	"strings"
	"testing"
)

func TestRuffPatternCaptureSkipsOnlyItsScript(t *testing.T) {
	command := ruffForTest(t)
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
		result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F821" || result.Diagnostics[0].Start.Line != 11 {
			t.Fatalf("capture template changed independent diagnostics: %+v", result.Diagnostics)
		}
	}
}

func TestRuffStatisticsRejected(t *testing.T) {
	command := ruffForTest(t)
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
	_, err := Analyze(t.Context(), AnalysisRequest{
		RuffOptions: &ExternalCommandOptions{Executable: &command, Arguments: []string{"--statistics"}},
		WorkingDir:  t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}},
	})
	if err == nil || !strings.Contains(err.Error(), "statistics output is not supported") {
		t.Fatalf("statistics mode not rejected before invocation: %v", err)
	}
}

func TestRuffFromTemplatePreservesFindings(t *testing.T) {
	command := ruffForTest(t)
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
					result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}}})
					if err != nil {
						t.Fatal(err)
					}
					if len(result.Diagnostics) == 0 {
						t.Fatal("from clause caused script checking to be skipped")
					}
					for _, finding := range result.Diagnostics {
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
	command := ruffForTest(t)
	for _, flag := range []string{"--silent", "-s", "-qs"} {
		source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
		_, err := Analyze(t.Context(), AnalysisRequest{
			RuffOptions: &ExternalCommandOptions{Executable: &command, Arguments: []string{flag}},
			WorkingDir:  t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}},
		})
		if err == nil || !strings.Contains(err.Error(), "silent output is not supported") {
			t.Fatalf("silent mode not rejected before invocation: %v", err)
		}
	}
}

func TestRuffOperatorTemplateSkipsOnlyItsScript(t *testing.T) {
	command := ruffForTest(t)
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
	result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F821" || result.Diagnostics[0].Start.Line != 10 {
		t.Fatalf("operator template changed independent diagnostics: %+v", result.Diagnostics)
	}
}

func TestRuffVersionedShellGrammar(t *testing.T) {
	command := ruffForTest(t)
	for _, tc := range []struct {
		config     string
		wantSyntax bool
	}{
		{"", true}, {"tools: {ruff: {target-version: py314}}", false},
	} {
		config, err := ParseConfig([]byte(tc.config))
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
		result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source), Config: config}}})
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
	command := ruffForTest(t)
	for _, option := range []string{"--output-file", "--output-file=", "-o", "-o=", "-oattached", "-qoattached"} {
		t.Run(option, func(t *testing.T) {
			root := t.TempDir()
			output := writeShellcheckFixture(t, root, "output.json", "preserve this file")
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
			_, err := Analyze(t.Context(), AnalysisRequest{
				RuffOptions: &ExternalCommandOptions{Executable: &command, Arguments: flags},
				WorkingDir:  root, Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}},
			})
			if err == nil || !strings.Contains(err.Error(), "output redirection") {
				t.Errorf("redirection not rejected: %v", err)
			}
			if content, err := os.ReadFile(output); err != nil || string(content) != "preserve this file" {
				t.Fatalf("explicit output file modified: %q, %v", content, err)
			}
		})
	}
}
