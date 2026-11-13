package results_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestAnalysisDiagnosticIdentity(t *testing.T) {
	t.Parallel()
	base := findingmodel.Finding{Path: "action.yml", Rule: "shellcheck", Message: "finding", Start: sourcepos.Position{Line: 1, Column: 1}, EndColumn: 4, Source: []byte("echo $VALUE\n")}
	var files []policy.File
	for _, change := range []func(*findingmodel.Finding){
		func(*findingmodel.Finding) {},
		func(*findingmodel.Finding) {},
		func(e *findingmodel.Finding) { e.EndColumn = 5 },
		func(e *findingmodel.Finding) { e.Source = []byte("echo $OTHER\n") },
		func(e *findingmodel.Finding) { e.Message = "another finding" },
		func(e *findingmodel.Finding) { e.Path = "another/action.yml" },
		func(e *findingmodel.Finding) { e.Rule = "another-rule" },
	} {
		finding := base
		change(&finding)
		files = append(files, policy.File{Content: []byte("workflow"), Findings: []*findingmodel.Finding{&finding}})
	}
	diagnostics, _ := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect(files)
	if len(diagnostics) != 6 || len(files) != 7 {
		t.Fatalf("only the identical diagnostic should be removed: %+v", diagnostics)
	}
}

func TestCollectProjectsSuppressionTarget(t *testing.T) {
	t.Parallel()
	finding := &findingmodel.Finding{Rule: "shellcheck", Message: "finding", Start: sourcepos.Position{Line: 2}, SuppressionTarget: &findingmodel.SuppressionOrigin{First: 2, Last: 3}}
	_, sources := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect([]policy.File{{InputPath: "workflow.yml", Findings: []*findingmodel.Finding{finding}}})
	if len(sources) != 1 || sources[0].Suppression == nil || *sources[0].Suppression != (report.SuppressionTarget{Path: "workflow.yml", First: 2, Last: 3}) {
		t.Fatalf("same-source target not resolved: %+v", sources)
	}
	finding.SuppressionTarget.Path = "action.yml"
	_, sources = (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect([]policy.File{{InputPath: "workflow.yml", Findings: []*findingmodel.Finding{finding}}})
	if len(sources) != 1 || sources[0].Suppression == nil || *sources[0].Suppression != (report.SuppressionTarget{Path: "action.yml", First: 2, Last: 3}) {
		t.Fatalf("foreign target not projected: %+v", sources)
	}
}

func TestMetadataParseDedupUsesCapturedIdentityAfterSymlinkRetarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first, second, alias := filepath.Join(root, "first.yml"), filepath.Join(root, "second.yml"), filepath.Join(root, "alias.yml")
	content := []byte("invalid action source")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(first, alias); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	store, fixture := capturefixture.NewCapturedSources(t, root)
	captured, err := fixture.Capture(alias, content, sourcepos.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	declaration := &findingmodel.Finding{Path: first, Rule: "syntax-check", Message: "invalid manifest"}
	caller := &findingmodel.Finding{Rule: "action", Message: "cannot parse manifest", MetadataParseSource: captured.Revision()}
	files := []policy.File{
		{InputPath: first, Source: captured.Revision(), Action: true, Content: content, Findings: []*findingmodel.Finding{declaration}},
		{InputPath: "ci.yml", Content: []byte("workflow"), Findings: []*findingmodel.Finding{caller}},
	}
	diagnostics, _ := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect(files)
	if len(diagnostics) != 1 || diagnostics[0].Message != declaration.Message || len(files[1].Findings) != 0 {
		t.Fatalf("post-seal path IO changed parse identity: %+v", diagnostics)
	}
}

func TestUnavailableReportedSourceNeverFabricatesRevisionOrFix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		content     []byte
		failed      bool
		reportedEnd sourcepos.Position
	}{
		{name: "unverified reread", content: []byte("echo $VALUE\n")},
		{name: "failed reread", content: []byte{}, failed: true},
		{name: "tool range retained", content: []byte("changed longer token\n"), reportedEnd: sourcepos.Position{Line: 1, Column: 12}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			finding := &findingmodel.Finding{
				Path: "reported.sh", Rule: "shellcheck", Message: "reported", Start: sourcepos.Position{Line: 1, Column: 6}, End: tc.reportedEnd,
				Reference: &sourcepos.Reference{Document: "must-not-survive", Revision: strings.Repeat("a", 64)}, Fixes: []diagmodel.Fix{{Revisions: map[string]string{"reported.sh": strings.Repeat("b", 64)}}},
				Source: tc.content, RawScriptSource: true, UnavailableSource: findingmodel.ReportedSourceUnavailable("shellcheck", "reported.sh", tc.failed),
			}
			diagnostics, _ := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect([]policy.File{{Content: []byte("workflow text"), Findings: []*findingmodel.Finding{finding}}})
			if len(diagnostics) != 1 || diagnostics[0].Reference != nil || len(diagnostics[0].Fixes) != 0 {
				t.Fatalf("unavailable source gained revision evidence: %+v", diagnostics)
			}
			wantEnd := tc.reportedEnd
			if wantEnd == (sourcepos.Position{}) {
				wantEnd = finding.Start
			}
			if diagnostics[0].End != wantEnd || tc.failed && diagnostics[0].Snippet != "" {
				t.Fatalf("reread fabricated extent/snippet: %+v", diagnostics[0])
			}
			encoded, err := json.Marshal(diagnostics[0])
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{`"reference"`, `"revision"`, `"revisions"`, `"fixes"`} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("published JSON fabricated %s: %s", forbidden, encoded)
				}
			}
		})
	}
}

func TestAnalysisDiagnosticMetadataIdentity(t *testing.T) {
	t.Parallel()
	base := findingmodel.Finding{Path: "action.yml", Rule: "shellcheck", Message: "finding", Start: sourcepos.Position{Line: 1, Column: 1}}
	var files []policy.File
	for _, change := range []func(*findingmodel.Finding){
		func(*findingmodel.Finding) {},
		func(e *findingmodel.Finding) { e.Code = "SC2086" },
		func(e *findingmodel.Finding) { e.Severity = "warning" },
		func(e *findingmodel.Finding) {
			e.Fixes = []diagmodel.Fix{{Description: "quote", Edits: []diagmodel.Edit{{Path: "action.yml", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 2}, Replacement: "quoted"}}}}
		},
	} {
		finding := base
		change(&finding)
		files = append(files, policy.File{Findings: []*findingmodel.Finding{&finding}}, policy.File{Findings: []*findingmodel.Finding{&finding}})
	}
	diagnostics, _ := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect(files)
	if len(diagnostics) != 4 {
		t.Fatalf("distinct analyzer metadata was lost: %+v", diagnostics)
	}
}

// Findings that point into the same foreign file share one copy of it in the
// reported sources, and that copy is not the findings' own Source.
func TestCollectCopiesEachForeignSourceOnce(t *testing.T) {
	t.Parallel()
	script := []byte("echo $A\necho $B\n")
	findings := []*findingmodel.Finding{
		{Path: "action.yml", Rule: "shellcheck", Message: "a", Start: sourcepos.Position{Line: 1, Column: 6}, Source: script},
		{Path: "action.yml", Rule: "shellcheck", Message: "b", Start: sourcepos.Position{Line: 2, Column: 6}, Source: script},
	}
	_, sources := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect([]policy.File{{Content: []byte("workflow"), Findings: findings}})
	if len(sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(sources))
	}
	if &sources[0].Content[0] != &sources[1].Content[0] {
		t.Error("findings in the same file got separate copies")
	}
	if &sources[0].Content[0] == &script[0] {
		t.Error("reported source shares storage with the finding")
	}
	if string(sources[0].Content) != string(script) {
		t.Errorf("content = %q, want %q", sources[0].Content, script)
	}
}
