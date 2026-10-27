package diagnosticpolicy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/source"
)

func TestAnalysisDiagnosticIdentity(t *testing.T) {
	base := findingmodel.Finding{Path: "action.yml", Rule: "shellcheck", Message: "finding", Start: diagmodel.Position{Line: 1, Column: 1}, EndColumn: 4, Source: []byte("echo $VALUE\n")}
	var files []File
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
		files = append(files, File{Content: []byte("workflow"), Findings: []*findingmodel.Finding{&finding}})
	}
	diagnostics, _ := Collect(files)
	if len(diagnostics) != 6 || len(files) != 7 {
		t.Fatalf("only the identical diagnostic should be removed: %+v", diagnostics)
	}
}

func TestMetadataParseDedupUsesCapturedIdentityAfterSymlinkRetarget(t *testing.T) {
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
	fixture := capturefixture.NewCapturedSources(t, root)
	captured, err := fixture.Capture(alias, content, source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.Store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	declaration := &findingmodel.Finding{Path: first, Rule: "syntax-check", Message: "invalid manifest"}
	caller := &findingmodel.Finding{Rule: "action", Message: "cannot parse manifest", MetadataParsePath: alias, MetadataParseSource: captured.Revision()}
	files := []File{
		{InputPath: first, Source: captured.Revision(), Action: true, Content: content, Findings: []*findingmodel.Finding{declaration}},
		{InputPath: "ci.yml", Content: []byte("workflow"), Findings: []*findingmodel.Finding{caller}},
	}
	diagnostics, _ := Collect(files)
	if len(diagnostics) != 1 || diagnostics[0].Message != declaration.Message || len(files[1].Findings) != 0 {
		t.Fatalf("post-seal path IO changed parse identity: %+v", diagnostics)
	}
}

func TestUnavailableReportedSourceNeverFabricatesRevisionOrFix(t *testing.T) {
	for _, tc := range []struct {
		name        string
		content     []byte
		failed      bool
		reportedEnd diagmodel.Position
	}{
		{name: "unverified reread", content: []byte("echo $VALUE\n")},
		{name: "failed reread", content: []byte{}, failed: true},
		{name: "tool range retained", content: []byte("changed longer token\n"), reportedEnd: diagmodel.Position{Line: 1, Column: 12}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finding := &findingmodel.Finding{Path: "reported.sh", Rule: "shellcheck", Message: "reported", Start: diagmodel.Position{Line: 1, Column: 6}, End: tc.reportedEnd,
				Reference: &source.Reference{Document: "must-not-survive", Revision: strings.Repeat("a", 64)}, Fixes: []diagmodel.Fix{{Revisions: map[string]string{"reported.sh": strings.Repeat("b", 64)}}},
				Source: tc.content, RawScriptSource: true, UnavailableSource: findingmodel.NativeShellcheckUnavailable("reported.sh", tc.failed),
			}
			diagnostics, _ := Collect([]File{{Content: []byte("workflow text"), Findings: []*findingmodel.Finding{finding}}})
			if len(diagnostics) != 1 || diagnostics[0].Reference != nil || len(diagnostics[0].Fixes) != 0 {
				t.Fatalf("unavailable source gained revision evidence: %+v", diagnostics)
			}
			wantEnd := tc.reportedEnd
			if wantEnd == (diagmodel.Position{}) {
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
	base := findingmodel.Finding{Path: "action.yml", Rule: "shellcheck", Message: "finding", Start: diagmodel.Position{Line: 1, Column: 1}}
	var files []File
	for _, change := range []func(*findingmodel.Finding){
		func(*findingmodel.Finding) {},
		func(e *findingmodel.Finding) { e.Code = "SC2086" },
		func(e *findingmodel.Finding) { e.Severity = "warning" },
		func(e *findingmodel.Finding) {
			e.Fixes = []diagmodel.Fix{{Description: "quote", Edits: []diagmodel.Edit{{Path: "action.yml", Start: diagmodel.Position{Line: 1, Column: 1}, End: diagmodel.Position{Line: 1, Column: 2}, Replacement: "quoted"}}}}
		},
	} {
		finding := base
		change(&finding)
		files = append(files,
			File{Findings: []*findingmodel.Finding{&finding}},
			File{Findings: []*findingmodel.Finding{&finding}},
		)
	}
	diagnostics, _ := Collect(files)
	if len(diagnostics) != 4 {
		t.Fatalf("distinct analyzer metadata was lost: %+v", diagnostics)
	}
}
