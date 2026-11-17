package results_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestAnalysisDiagnosticIdentity(t *testing.T) {
	t.Parallel()
	base := finding.Finding{Path: "action.yml", Rule: "shellcheck", Message: "finding", Start: sourcetext.Position{Line: 1, Column: 1}, End: sourcetext.Position{Line: 1, Column: 5}, Source: results.DiagnosticSource{Content: []byte("echo $VALUE\n")}}
	var files []policy.Batch
	for _, change := range []func(*finding.Finding){
		func(*finding.Finding) {},
		func(*finding.Finding) {},
		func(e *finding.Finding) { e.End.Column = 6 },
		func(e *finding.Finding) { e.Source.Content = []byte("echo $OTHER\n") },
		func(e *finding.Finding) { e.Message = "another finding" },
		func(e *finding.Finding) { e.Path = "another/action.yml" },
		func(e *finding.Finding) { e.Rule = "another-rule" },
	} {
		item := base
		change(&item)
		files = append(files, policy.Batch{Policy: policy.DocumentInputs{Content: []byte("workflow")}, Findings: []*finding.Finding{&item}})
	}
	published := publishBatches(t, files)
	diagnostics := published.Diagnostics
	if len(diagnostics) != 6 || len(files) != 7 {
		t.Fatalf("only the identical diagnostic should be removed: %+v", diagnostics)
	}
}

func TestCollectProjectsSuppressionTarget(t *testing.T) {
	t.Parallel()
	item := &finding.Finding{Rule: "shellcheck", Message: "finding", Start: sourcetext.Position{Line: 2}, SuppressionOrigin: &results.SuppressionOrigin{Lines: sourcetext.LineRange{First: 2, Last: 3}}}
	reported := publishBatches(t, []policy.Batch{{InputPath: "workflow.yml", Policy: policy.DocumentInputs{Content: []byte("name: test\nrun: echo hi\n")}, Findings: []*finding.Finding{item}}}).Diagnostics
	if len(reported) != 1 || reported[0].Source.Suppression == nil || *reported[0].Source.Suppression != (results.SuppressionOrigin{Path: "workflow.yml", Lines: sourcetext.LineRange{First: 2, Last: 3}}) {
		t.Fatalf("same-source target not resolved: %+v", reported)
	}
	item.Path = "action.yml"
	item.Source.Content = []byte("name: action\nrun: echo hi\n")
	item.SuppressionOrigin.Path = "action.yml"
	batch := preparePublicationBatch(t, policy.Batch{InputPath: "workflow.yml", Policy: policy.DocumentInputs{Content: []byte("name: workflow\n")}, Findings: []*finding.Finding{item}, Sources: map[string][]byte{"action.yml": item.Source.Content}}, nil, map[string]*configuration.Config{"action.yml": nil})
	published, err := policy.Publish([]policy.Batch{batch}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reported = published.Diagnostics
	if len(reported) != 1 || reported[0].Source.Suppression == nil || *reported[0].Source.Suppression != (results.SuppressionOrigin{Path: "action.yml", Lines: sourcetext.LineRange{First: 2, Last: 3}}) {
		t.Fatalf("foreign target not projected: %+v", reported)
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
	captured, err := fixture.Capture(alias, content, sourcetext.YAML)
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
	declaration := &finding.Finding{Path: first, Rule: "syntax-check", Message: "invalid manifest"}
	caller := &finding.Finding{Rule: "action", Message: "cannot parse manifest", MetadataParseSource: captured.Revision()}
	files := []policy.Batch{
		{InputPath: first, Source: captured.Revision(), Action: true, Policy: policy.DocumentInputs{Content: content}, Findings: []*finding.Finding{declaration}},
		{InputPath: "ci.yml", Policy: policy.DocumentInputs{Content: []byte("workflow")}, Findings: []*finding.Finding{caller}},
	}
	published := publishBatches(t, files)
	diagnostics := published.Diagnostics
	if len(diagnostics) != 1 || diagnostics[0].Message != declaration.Message || len(published.Findings[1]) != 0 {
		t.Fatalf("post-seal path IO changed parse identity: %+v", diagnostics)
	}
}

func TestUnavailableReportedSourceNeverFabricatesRevisionOrFix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		content     []byte
		failed      bool
		reportedEnd sourcetext.Position
	}{
		{name: "unverified reread", content: []byte("echo $VALUE\n")},
		{name: "failed reread", content: []byte{}, failed: true},
		{name: "tool range retained", content: []byte("changed longer token\n"), reportedEnd: sourcetext.Position{Line: 1, Column: 12}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := &finding.Finding{
				Path: "reported.sh", Rule: "shellcheck", Message: "reported", Start: sourcetext.Position{Line: 1, Column: 6}, End: tc.reportedEnd,
				Reference: &sourcetext.Reference{Document: "must-not-survive", Revision: strings.Repeat("a", 64)}, Fixes: []results.Fix{{Revisions: map[string]string{"reported.sh": strings.Repeat("b", 64)}}},
				Source: results.DiagnosticSource{Content: tc.content, RawScript: true, Unavailable: results.ReportedSourceUnavailable("shellcheck", "reported.sh", tc.failed)},
			}
			diagnostics := publishBatches(t, []policy.Batch{{Policy: policy.DocumentInputs{Content: []byte("workflow text")}, Findings: []*finding.Finding{item}}}).Diagnostics
			if len(diagnostics) != 1 || diagnostics[0].Reference != nil || len(diagnostics[0].Fixes) != 0 {
				t.Fatalf("unavailable source gained revision evidence: %+v", diagnostics)
			}
			wantEnd := tc.reportedEnd
			if wantEnd == (sourcetext.Position{}) {
				wantEnd = item.Start
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
	base := finding.Finding{Path: "action.yml", Rule: "shellcheck", Message: "finding", Start: sourcetext.Position{Line: 1, Column: 1}}
	var files []policy.Batch
	for _, change := range []func(*finding.Finding){
		func(*finding.Finding) {},
		func(e *finding.Finding) { e.Code = "SC2086" },
		func(e *finding.Finding) { e.Severity = "warning" },
		func(e *finding.Finding) {
			e.Fixes = []results.Fix{{Description: "quote", Edits: []results.Edit{{Path: "action.yml", Start: sourcetext.Position{Line: 1, Column: 1}, End: sourcetext.Position{Line: 1, Column: 2}, Replacement: "quoted"}}}}
		},
	} {
		item := base
		change(&item)
		files = append(files, policy.Batch{Findings: []*finding.Finding{&item}}, policy.Batch{Findings: []*finding.Finding{&item}})
	}
	published := publishBatches(t, files)
	diagnostics := published.Diagnostics
	if len(diagnostics) != 4 {
		t.Fatalf("distinct analyzer metadata was lost: %+v", diagnostics)
	}
}

// Findings that point into the same foreign file share one copy of it in the
// reported sources, and that copy is not the findings' own Source.
func TestCollectCopiesEachForeignSourceOnce(t *testing.T) {
	t.Parallel()
	script := []byte("echo $A\necho $B\n")
	findings := []*finding.Finding{
		{Path: "action.yml", Rule: "shellcheck", Message: "a", Start: sourcetext.Position{Line: 1, Column: 6}, Source: results.DiagnosticSource{Content: script}},
		{Path: "action.yml", Rule: "shellcheck", Message: "b", Start: sourcetext.Position{Line: 2, Column: 6}, Source: results.DiagnosticSource{Content: script}},
	}
	reported := publishBatches(t, []policy.Batch{{Policy: policy.DocumentInputs{Content: []byte("workflow")}, Findings: findings}}).Diagnostics
	if len(reported) != 2 {
		t.Fatalf("sources = %d, want 2", len(reported))
	}
	if &reported[0].Source.Content[0] != &reported[1].Source.Content[0] {
		t.Error("findings in the same file got separate copies")
	}
	if &reported[0].Source.Content[0] == &script[0] {
		t.Error("reported source shares storage with the finding")
	}
	if string(reported[0].Source.Content) != string(script) {
		t.Errorf("content = %q, want %q", reported[0].Source.Content, script)
	}
}
