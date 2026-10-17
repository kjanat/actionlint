package diagnosticpolicy

import (
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	findingmodel "actionlint.kjanat.dev/internal/finding"
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
