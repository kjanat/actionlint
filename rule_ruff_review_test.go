package actionlint

import (
	"os"
	"strings"
	"testing"
)

func TestRuffPatternCaptureSkipsOnlyItsScript(t *testing.T) {
	command := ruffForTest(t)
	for _, pattern := range []string{`{"x": x, **${{ 'rest' }}}`, `[*${{ 'rest' }}]`, `${{ 'Widget' }}()`, `Outer(${{ 'Widget' }}())`} {
		source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: |\n          match {}:\n            case " + pattern + ": pass\n      - shell: python\n        run: print(missing)\n"
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
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"
	_, err := Analyze(t.Context(), AnalysisRequest{
		RuffOptions: &ExternalCommandOptions{Executable: &command, Arguments: []string{"--statistics"}},
		WorkingDir:  t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}},
	})
	if err == nil || !strings.Contains(err.Error(), "statistics output is not supported") {
		t.Fatalf("statistics mode not rejected before invocation: %v", err)
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
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"
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
