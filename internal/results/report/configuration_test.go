package report_test

import (
	"encoding/json"
	"testing"

	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestConfigurationSelectionJSON(t *testing.T) {
	t.Parallel()
	// Configuration selections expose their file, overlays and source metadata.
	selected := configreport.Report{
		Project: "project", File: "actionlint.yml", Explicit: true,
		Overrides: []string{"tools"},
		Inspection: configreport.Inspection{
			Path: "actionlint.yml", Config: map[string]any{"tools": false},
			Origins: map[string]configreport.Origin{
				"/tools": {Source: "input", Input: "tools", State: "value", Line: 2, Column: 3},
			},
			Warnings: []configreport.Warning{{File: "base.yml", Message: "ignored setting", Line: 4, Column: 5}},
		},
	}
	result := report.FromAnalysis(report.Analysis{Configurations: []configreport.Report{selected}})
	got, err := json.Marshal(result.Configs)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"file":"actionlint.yml","project":"project","overrides":["tools"],"origins":{"/tools":{"source":"input","input":"tools","state":"value","line":2,"column":3}},"warnings":[{"file":"base.yml","message":"ignored setting","line":4,"column":5}]}]`
	if string(got) != want {
		t.Fatalf("configuration JSON = %s, want %s", got, want)
	}
	// Inspection JSON also includes the decoded configuration.
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
	t.Parallel()
	for _, tc := range []struct {
		overrides []string
		json      string
	}{
		{nil, "null"},
		{[]string{}, "[]"},
	} {
		t.Run(tc.json, func(t *testing.T) {
			t.Parallel()
			result := report.FromAnalysis(report.Analysis{})
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
