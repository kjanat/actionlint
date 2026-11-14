package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestCLIIgnoreSelectors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	config := filepath.Join(root, "actionlint.yaml")
	if err := os.WriteFile(config, []byte("tools: {shellcheck: false, ruff: false}\nlint:\n  rules:\n    preset: none\n    correctness:\n      expression-reference: error\n    security:\n      missing-permissions: error\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "on: push\njobs:\n  test:\n    name: ${{ github.missing }}\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"unfiltered", []string{"check"}, []string{"expression-reference", "missing-permissions"}},
		{"rule", []string{"check", "--ignore", "missing-permissions"}, []string{"expression-reference"}},
		{"code", []string{"check", "--ignore", "unknown-property"}, []string{"missing-permissions"}},
		{"qualified-code", []string{"check", "--ignore", "expression-reference/unknown-property"}, []string{"missing-permissions"}},
		{"wrong-rule", []string{"check", "--ignore", "shellcheck/unknown-property"}, []string{"expression-reference", "missing-permissions"}},
		{"repeated", []string{"check", "--ignore", "missing-permissions", "--ignore", "unknown-property"}, nil},
		{"duplicate", []string{"check", "--ignore", "missing-permissions", "--ignore", "missing-permissions"}, []string{"expression-reference"}},
		{"before-command", []string{"--ignore", "missing-permissions", "check", "--ignore", "unknown-property"}, nil},
		{"regex-and-selector", []string{"--ignore-regex", "^unknown-property:", "check", "--ignore", "missing-permissions"}, nil},
		{"root", []string{"--ignore", "missing-permissions", "--ignore", "unknown-property"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out, log bytes.Buffer
			args := append([]string{"actionlint"}, tc.args...)
			args = append(args, "--offline", "--config", config, "--json", "-")
			status := (&Command{Stdin: strings.NewReader(source), Stdout: &out, Stderr: &log}).MainContext(t.Context(), args)
			wantStatus := 0
			if len(tc.want) != 0 {
				wantStatus = 1
			}
			if status != wantStatus {
				t.Fatalf("status=%d want=%d stderr=%s stdout=%s", status, wantStatus, log.String(), out.String())
			}
			var result resultreport.CheckResult
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, diagnostic := range result.Diagnostics {
				got = append(got, diagnostic.Rule)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("rules=%v want=%v diagnostics=%+v", got, tc.want, result.Diagnostics)
			}
			var summary struct {
				Summary resultreport.CheckSummary `json:"summary"`
			}
			if err := json.Unmarshal(log.Bytes(), &summary); err != nil {
				t.Fatalf("summary: %v: %s", err, log.String())
			}
			if summary.Summary.Findings != len(tc.want) {
				t.Fatalf("summary=%+v, want %d findings", summary.Summary, len(tc.want))
			}
		})
	}
	for _, format := range []string{"text", "pretty", "oneline", "gcc", "json", "jsonl", "sarif", "github"} {
		t.Run("format/"+format, func(t *testing.T) {
			t.Parallel()
			var out, log bytes.Buffer
			args := []string{"actionlint", "check", "--config", config, "--offline", "--ignore", "missing-permissions", "--ignore", "unknown-property", "--output-format", format, "-"}
			status := (&Command{Stdin: strings.NewReader(source), Stdout: &out, Stderr: &log}).MainContext(t.Context(), args)
			wantSummary := "All clear!"
			if format == "json" || format == "jsonl" {
				wantSummary = `"findings":0`
			}
			if status != 0 || strings.Contains(out.String(), `property "missing" is not defined`) || strings.Contains(out.String(), "::error") || !strings.Contains(log.String(), wantSummary) {
				t.Fatalf("status=%d stderr=%s stdout=%s", status, log.String(), out.String())
			}
		})
	}
}

func TestCLIIgnoreWithEnvironmentRegex(t *testing.T) {
	t.Setenv("ACTIONLINT_IGNORE_REGEX", `["second"]`)
	app := newCommandApp(t.Context(), &Command{})
	if _, _, err := app.parseRoot([]string{"--ignore", "first"}); err != nil {
		t.Fatal(err)
	}
	if err := app.applyEnvironment(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(app.inv.Check.IgnoreRegex, []string{"second"}) || !slices.Equal(app.inv.Check.IgnoreSelectors, []string{"first"}) {
		t.Fatalf("regex=%v selectors=%v", app.inv.Check.IgnoreRegex, app.inv.Check.IgnoreSelectors)
	}
}

func TestCLIIgnoreInvalidSelector(t *testing.T) {
	t.Parallel()
	var out, log bytes.Buffer
	status := (&Command{Stdout: &out, Stderr: &log}).MainContext(t.Context(), []string{"actionlint", "check", "--ignore", "", "-"})
	if status != 2 || !strings.Contains(log.String(), "invalid --ignore selector") {
		t.Fatalf("status=%d stderr=%s stdout=%s", status, log.String(), out.String())
	}
}

func TestCLIIgnoreExcludesFixes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yml")
	config := filepath.Join(root, "actionlint.yaml")
	source := []byte("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok # ignore[expression-reference]\n")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("tools: {shellcheck: false, ruff: false}\nlint: {rules: {preset: none, correctness: {expression-reference: error, unused-suppression: info}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ignored := range []bool{true, false} {
		var out, log bytes.Buffer
		args := []string{"actionlint", "fix", "--apply", "--offline", "--config", config, path}
		if ignored {
			args = append(args, "--ignore", "unused-suppression")
		}
		status := (&Command{Stdout: &out, Stderr: &log}).MainContext(t.Context(), args)
		if status != 0 {
			t.Fatalf("ignored=%t status=%d stderr=%s stdout=%s", ignored, status, log.String(), out.String())
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(got, source) != ignored {
			t.Fatalf("ignored=%t source=%s", ignored, got)
		}
	}
}
