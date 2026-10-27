package application_test

import (
	"context"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
)

type actionSourceReferenceProbe struct{ path string }

func (actionSourceReferenceProbe) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "action-source-reference", Requires: []analyzer.Fact{analyzer.Source}}
}

func (p actionSourceReferenceProbe) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return []diagnostic.Diagnostic{{Rule: "action-source-reference", Message: "source reference", Path: p.path, Start: diagnostic.Position{Line: 1, Column: 1}}}, nil
}

func TestFetchedActionFindingsReceiveCanonicalSourceReference(t *testing.T) {
	for _, tc := range []struct {
		name       string
		manifest   string
		suppressed bool
	}{
		{"non-composite", "name: node\ndescription: node action\nruns: {using: node20, main: index.js}\n", false},
		{"parse-failed", "name: recovered\ndescription: partial\ninputs: [wrong]\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo partial\n", false},
		{"fetched-source-policy", "name: node # actionlint:ignore action-source-reference -- reviewed\ndescription: node action\nruns: {using: node20, main: index.js}\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			actionPath := writeFixture(t, root, "local/action.yml", tc.manifest)
			workflowPath := writeFixture(t, root, "ci.yml", "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/local}]}}\n")
			result, err := application.Check(t.Context(), application.Options{
				WorkingDir: root,
				Analyzers:  []analyzer.Registration{{Analyzer: actionSourceReferenceProbe{path: actionPath}}},
			}, application.Input{Kind: application.Files, Paths: []string{workflowPath}})
			if err != nil {
				t.Fatal(err)
			}
			for _, finding := range result.Diagnostics {
				if finding.Rule != "action-source-reference" {
					continue
				}
				if tc.suppressed {
					t.Fatalf("fetched source policy did not suppress its finding: %+v", finding)
				}
				if finding.Reference == nil {
					t.Fatal("fetched action finding has no canonical source reference")
				}
				if got, want := finding.Reference.Document, filepath.ToSlash(actionPath); got != want {
					t.Fatalf("reference document = %q, want %q", got, want)
				}
				return
			}
			if tc.suppressed {
				return
			}
			t.Fatalf("source analyzer finding absent: %+v", result.Diagnostics)
		})
	}
}
