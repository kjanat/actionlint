package output_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	sarifcheck "actionlint.kjanat.dev/v2/internal/development/tests/support/sarif"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	render "actionlint.kjanat.dev/v2/internal/output"

	"github.com/google/go-cmp/cmp"
)

func TestSARIFDefaultsDoNotFollowCategoryOverride(t *testing.T) {
	t.Parallel()
	root, _ := testutil.ExecutableFixture(t)
	file := filepath.Join("testdata", "format", "test.yaml")
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fixturefiles.Write(t, root, file, string(content))
	cfg, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: {correctness: warn}}"))
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.Source{{Path: file, InputPath: filepath.Join(root, file), Content: content, Project: workspace.Identity(root), Config: cfg}})
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := render.New(render.FormatSARIF, "", false, func() string { return "test" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := renderer.Render(&output, render.FromAnalysis(*analysis)); err != nil {
		t.Fatal(err)
	}
	sarifcheck.RequireValid(t, []byte(output.String()))
	var doc struct {
		Runs []struct {
			Tool struct {
				Driver struct {
					Rules []struct {
						ID      string `json:"id"`
						Default struct {
							Level   string `json:"level"`
							Enabled bool   `json:"enabled"`
						} `json:"defaultConfiguration"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID string `json:"ruleId"`
				Level  string `json:"level"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(output.String()), &doc); err != nil {
		t.Fatal(err)
	}
	var foundRule, foundResult bool
	for _, r := range doc.Runs[0].Tool.Driver.Rules {
		if r.ID == "expression-reference" {
			foundRule = true
			if r.Default.Level != "error" || !r.Default.Enabled {
				t.Fatalf("rule default = %+v", r.Default)
			}
		}
	}
	for _, result := range doc.Runs[0].Results {
		if result.RuleID == "expression-reference" {
			foundResult = true
			if result.Level != "warning" {
				t.Fatalf("result level = %q, want warning", result.Level)
			}
		}
	}
	if !foundRule || !foundResult {
		t.Fatalf("rule found = %t, result found = %t", foundRule, foundResult)
	}
}

func TestWorkflowDiagnosticTemplates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file   string
		format string
	}{
		{
			file:   "test.json",
			format: "{{json .}}",
		},
		{
			file:   "test.jsonl",
			format: "{{range $err := .}}{{json $err}}{{end}}",
		},
		{
			file:   "test.md",
			format: "{{range $ := .}}### Error at line {{$.Line}}, col {{$.Column}} of `{{$.Filepath}}`\\n\\n{{$.Message}}\\n\\n```\\n{{$.Snippet}}\\n```\\n\\n{{end}}",
		},
	}

	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("testdata", "format")
	proj := workspace.Identity(filepath.Join(root, dir))
	infile := filepath.Join(dir, "test.yaml")
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			content, err := os.ReadFile(filepath.Join(root, infile))
			if err != nil {
				t.Fatal(err)
			}
			result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.Source{{Path: infile, InputPath: filepath.Join(root, infile), Content: content, Project: proj, Config: &repoconfig.Config{}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) == 0 {
				t.Fatal("no diagnostics")
			}
			renderer, err := render.New("", tc.format, false, func() string { return "test" }, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := renderer.Render(&b, render.FromAnalysis(*result)); err != nil {
				t.Fatal(err)
			}
			buf, err := os.ReadFile(filepath.Join(root, dir, tc.file))
			if err != nil {
				panic(err)
			}
			want := string(buf)

			have := b.String()
			// Fix path separators on Windows
			if runtime.GOOS == "windows" {
				slash := filepath.ToSlash(infile)
				have = strings.ReplaceAll(have, infile, slash)
				escaped := strings.ReplaceAll(slash, "/", `\\`)
				have = strings.ReplaceAll(have, escaped, slash)
			}

			if diff := cmp.Diff(want, have); diff != "" {
				t.Logf("have: %s", have)
				t.Fatal(diff)
			}
		})
	}
}

func TestWorkflowDiagnosticSARIF(t *testing.T) {
	t.Parallel()
	fixtureRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := testutil.ExecutableFixture(t)
	dir := filepath.Join("testdata", "format")
	proj := workspace.Identity(root)
	file := filepath.Join(dir, "test.yaml")

	var b strings.Builder
	content, err := os.ReadFile(filepath.Join(fixtureRoot, file))
	if err != nil {
		t.Fatal(err)
	}
	fixturefiles.Write(t, root, file, string(content))
	result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.Source{{Path: file, InputPath: filepath.Join(root, file), Content: content, Project: proj, Config: &repoconfig.Config{}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("no diagnostics")
	}
	renderer, err := render.New(render.FormatSARIF, "", false, func() string { return "(devel)" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := renderer.Render(&b, render.FromAnalysis(*result)); err != nil {
		t.Fatal(err)
	}
	sarifcheck.RequireValid(t, []byte(b.String()))
	var document struct {
		Runs []struct {
			ColumnKind string
			Tool       struct {
				Driver struct {
					Name  string
					Rules []struct{ ID string }
				}
			}
			Results []struct {
				RuleID, Level string
				Message       struct{ Text string }
				Properties    struct{ DiagnosticCode string }
				Locations     []struct {
					PhysicalLocation struct {
						ArtifactLocation struct{ URI, URIBaseID string }
						Region           struct {
							StartLine, StartColumn, EndLine, EndColumn int
							Snippet                                    struct{ Text string }
						}
					}
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(b.String()), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Runs) != 1 {
		t.Fatalf("got %d SARIF runs, want one", len(document.Runs))
	}
	run := document.Runs[0]
	if run.ColumnKind != "unicodeCodePoints" || run.Tool.Driver.Name != "GitHub Actions lint" {
		t.Fatalf("columnKind=%q tool=%q", run.ColumnKind, run.Tool.Driver.Name)
	}
	want := []struct {
		rule, code, message     string
		line, column, endColumn int
	}{
		{"syntax-check", "", `unexpected key "branch"`, 3, 5, 12},
		{"expression-reference", "unknown-property", `property "msg" is not defined in matrix`, 9, 23, 33},
		{"syntax-check", "", `unexpected key "with"`, 10, 9, 14},
	}
	if len(run.Results) != len(want) {
		t.Fatalf("got %d findings, want %d: %+v", len(run.Results), len(want), run.Results)
	}
	lines := strings.Split(string(content), "\n")
	for i, expected := range want {
		got := run.Results[i]
		if got.RuleID != expected.rule || got.Properties.DiagnosticCode != expected.code || got.Level != "error" || !strings.Contains(got.Message.Text, expected.message) {
			t.Errorf("finding %d: rule=%q code=%q level=%q message=%q, want %q/%q error containing %q", i, got.RuleID, got.Properties.DiagnosticCode, got.Level, got.Message.Text, expected.rule, expected.code, expected.message)
		}
		if len(got.Locations) != 1 {
			t.Fatalf("finding %d has %d locations, want one", i, len(got.Locations))
		}
		location := got.Locations[0].PhysicalLocation
		if location.ArtifactLocation.URI != "testdata/format/test.yaml" || location.ArtifactLocation.URIBaseID != "%SRCROOT%" {
			t.Errorf("finding %d points to %+v", i, location.ArtifactLocation)
		}
		region := location.Region
		if region.StartLine != expected.line || region.EndLine != expected.line || region.StartColumn != expected.column || region.EndColumn != expected.endColumn || region.Snippet.Text != strings.TrimSuffix(lines[expected.line-1], "\r") {
			t.Errorf("finding %d region=%+v, want line %d columns %d-%d and its source text", i, region, expected.line, expected.column, expected.endColumn)
		}
		declared := false
		for _, rule := range run.Tool.Driver.Rules {
			if rule.ID == got.RuleID {
				declared = true
				break
			}
		}
		if !declared {
			t.Errorf("finding %d references undeclared rule %q", i, got.RuleID)
		}
	}
}
