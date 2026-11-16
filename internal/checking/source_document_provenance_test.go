package checking_test

import (
	"context"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/results"
)

type actionSourceReferenceProbe struct{ path string }

func (actionSourceReferenceProbe) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "action-source-reference", Category: "correctness", RequiredInputs: []rule.InputKind{rule.Source}}
}

func (p actionSourceReferenceProbe) AnalyzeDocument(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return []results.Diagnostic{{Rule: "action-source-reference", Message: "source reference", Path: p.path, Start: sourcetext.Position{Line: 1, Column: 1}}}, nil
}

func TestFetchedActionFindingsReceiveCanonicalSourceReference(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			root, _ := executableFixture(t)
			actionPath := writeFixture(t, root, "local/action.yml", tc.manifest)
			workflowPath := writeFixture(t, root, "ci.yml", "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/local}]}}\n")
			result, err := checking.Check(t.Context(), checking.Options{
				WorkingDir: root,
				Analyzers:  []checkinputs.Registration{{Analyzer: actionSourceReferenceProbe{path: actionPath}}},
			}, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{workflowPath}})
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
