package actionlint

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataIgnoresAcrossWorkflows(t *testing.T) {
	for _, tc := range []struct {
		name, config, firstIgnore string
		wantPaths, wantSeverities []string
	}{
		{"shared-finding", "", "", []string{".github/workflows/a.yml"}, []string{""}},
		{"first-inline-ignored", "", "      # actionlint:ignore-next-line action -- covered elsewhere\n", []string{".github/workflows/b.yml"}, []string{""}},
		{"first-file-ignored", "ignores: [{rule: action, file: '.github/workflows/a.yml'}]\n", "", []string{".github/workflows/b.yml"}, []string{""}},
		{"first-rule-off", "overrides:\n  - includes: ['.github/workflows/a.yml']\n    lint: {rules: {correctness: {action: off}}}\n", "", []string{".github/workflows/b.yml"}, []string{""}},
		{"different-severities", "lint: {rules: {correctness: {action: error}}}\noverrides:\n  - includes: ['.github/workflows/a.yml']\n    lint: {rules: {correctness: {action: warn}}}\n", "", []string{".github/workflows/a.yml", ".github/workflows/b.yml"}, []string{"warning", "error"}},
		{"all-ignored", "ignores: [{rule: action, file: '.github/workflows/*.yml'}]\n", "", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n"+tc.config)
			writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
			workflow := func(ignore string) string {
				return "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" + ignore + "      - uses: ./inner\n"
			}
			first := writeShellcheckFixture(t, root, ".github/workflows/a.yml", workflow(tc.firstIgnore))
			second := writeShellcheckFixture(t, root, ".github/workflows/b.yml", workflow(""))
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			for attempt := range 3 {
				result, err := session.Files([]string{first, second}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != len(tc.wantPaths) {
					t.Fatalf("attempt %d: expected %d findings, got %+v", attempt, len(tc.wantPaths), result.Diagnostics)
				}
				for i, d := range result.Diagnostics {
					if d.Rule != "action" || !strings.Contains(d.Message, "description is required") || filepath.ToSlash(d.Path) != tc.wantPaths[i] || d.Severity != tc.wantSeverities[i] {
						t.Fatalf("attempt %d: finding %d = %+v; expected %s with severity %s", attempt, i, d, tc.wantPaths[i], tc.wantSeverities[i])
					}
				}
			}
		})
	}
}

func TestMetadataDedupKeepsInvocationErrorsAcrossWorkflows(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
	writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ninputs:\n  token:\n    required: true\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
	const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n      - uses: ./inner\n"
	first := writeShellcheckFixture(t, root, ".github/workflows/a.yml", workflow)
	second := writeShellcheckFixture(t, root, ".github/workflows/b.yml", workflow)
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{first, second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata := 0
	invocations := make(map[string][]int)
	for _, d := range result.Diagnostics {
		switch {
		case strings.Contains(d.Message, "description is required"):
			metadata++
		case strings.Contains(d.Message, `missing input "token"`):
			invocations[filepath.ToSlash(d.Path)] = append(invocations[filepath.ToSlash(d.Path)], d.Start.Line)
		default:
			t.Fatalf("unexpected finding: %+v", d)
		}
	}
	if metadata != 1 {
		t.Fatalf("expected one metadata finding, got %d: %+v", metadata, result.Diagnostics)
	}
	for _, path := range []string{".github/workflows/a.yml", ".github/workflows/b.yml"} {
		lines := invocations[path]
		if len(lines) != 2 || lines[0] != 6 || lines[1] != 7 {
			t.Fatalf("missing per-invocation findings for %s: %v", path, lines)
		}
	}
}

func TestMetadataDedupKeepsDistinctSuppressionViolations(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\npolicy: {disallow-suppressions: {report: suppression}}\nignores:\n  - {rule: action, file: '.github/workflows/a.yml'}\n  - {rule: action, file: '.github/workflows/b.yml'}\n")
	writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
	const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n"
	first := writeShellcheckFixture(t, root, ".github/workflows/a.yml", workflow)
	second := writeShellcheckFixture(t, root, ".github/workflows/b.yml", workflow)
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{first, second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("expected both forbidden suppressions, got %+v", result.Diagnostics)
	}
	for i, finding := range result.Diagnostics {
		path := []string{".github/workflows/a.yml", ".github/workflows/b.yml"}[i]
		if finding.Rule != "disallow-suppressions" || filepath.ToSlash(finding.Path) != path {
			t.Fatalf("expected suppression policy finding at %s, got %+v", path, finding)
		}
	}
}
