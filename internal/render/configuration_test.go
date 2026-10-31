package render_test

import (
	"encoding/json"
	"testing"

	config "actionlint.kjanat.dev/v2/config/report"
	configreport "actionlint.kjanat.dev/v2/config/report"
	"actionlint.kjanat.dev/v2/internal/render"
	"actionlint.kjanat.dev/v2/internal/report"
)

func TestConfigurationReportPublicAliasesAndJSON(t *testing.T) {
	// Existing callers can keep constructing the public config aliases and pass
	// them directly through the completed report and render contracts.
	selected := config.Report{
		Project: "project", File: "actionlint.yml", Explicit: true,
		Overrides: []string{"tools"},
		Inspection: config.Inspection{
			Path: "actionlint.yml", Config: map[string]any{"tools": false},
			Origins: map[string]config.Origin{
				"/tools": {Source: "input", Input: "tools", State: "value", Line: 2, Column: 3},
			},
			Warnings: []config.Warning{{File: "base.yml", Message: "ignored setting", Line: 4, Column: 5}},
		},
	}
	result := render.Prepare(report.Analysis{Configurations: []configreport.Report{selected}}).CheckResult()
	got, err := json.Marshal(result.Configs)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"file":"actionlint.yml","project":"project","overrides":["tools"],"origins":{"/tools":{"source":"input","input":"tools","state":"value","line":2,"column":3}},"warnings":[{"file":"base.yml","message":"ignored setting","line":4,"column":5}]}]`
	if string(got) != want {
		t.Fatalf("configuration JSON = %s, want %s", got, want)
	}
	// The full inspection remains available separately from the result's
	// selection provenance, with its existing lowercase field names.
	got, err = json.Marshal(selected.Inspection)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"path":"actionlint.yml","config":{"tools":false},"origins":{"/tools":{"source":"input","input":"tools","state":"value","line":2,"column":3}},"warnings":[{"file":"base.yml","message":"ignored setting","line":4,"column":5}]}`
	if string(got) != want {
		t.Fatalf("inspection JSON = %s, want %s", got, want)
	}
	inspectionJSON := want
	got, err = json.Marshal(selected)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"Project":"project","File":"actionlint.yml","Explicit":true,"Overrides":["tools"],"Inspection":` + inspectionJSON + `}`
	if string(got) != want {
		t.Fatalf("library configuration report JSON = %s, want %s", got, want)
	}
}

func TestConfigurationReportWithoutProvenanceJSON(t *testing.T) {
	for _, tc := range []struct {
		overrides []string
		json      string
	}{
		{nil, "null"},
		{[]string{}, "[]"},
	} {
		t.Run(tc.json, func(t *testing.T) {
			result := render.NewCheckResult(0)
			result.AddConfiguration(configreport.Report{Overrides: tc.overrides})
			got, err := json.Marshal(result.Configs)
			if err != nil {
				t.Fatal(err)
			}
			want := `[{"file":"","project":"","overrides":` + tc.json + `}]`
			if string(got) != want {
				t.Fatalf("configuration JSON = %s, want %s", got, want)
			}
		})
	}
}
