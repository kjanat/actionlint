package output

import (
	"bytes"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestNativeCodePresentation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ code, prefix string }{
		{"SC2086", "SC2086: "},
		{"", ""},
		{"shellcheck", ""},
	} {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			finding := FindingOf(results.Diagnostic{Rule: "shellcheck", Code: tc.code, Path: "workflow.yml", Start: sourcepos.Position{Line: 2, Column: 3}, Message: "Quote this value"}, nil)
			var output bytes.Buffer
			finding.PrettyPrint(&output)
			if got, want := output.String(), "workflow.yml:2:3: "+tc.prefix+"Quote this value [shellcheck]\n"; got != want {
				t.Fatalf("text output=%q, want %q", got, want)
			}
			fields := finding.TemplateFields()
			if fields.Code != tc.code || fields.Message != "Quote this value" {
				t.Fatalf("template fields conflate code and message: %+v", fields)
			}
			formatter, err := NewFormatter("{{range .}}{{.Kind}}/{{.Code}}: {{.Message}}{{end}}", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			output.Reset()
			if err := formatter.Print(&output, []*TemplateFields{fields}); err != nil {
				t.Fatal(err)
			}
			if got, want := output.String(), "shellcheck/"+tc.code+": Quote this value"; got != want {
				t.Fatalf("template output=%q, want %q", got, want)
			}
		})
	}
}
