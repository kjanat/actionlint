package core

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
)

func TestEncodeRequestPerCheckVersion(t *testing.T) {
	request := analyzer.ScriptRequest{Source: "echo", Filename: "s.sh", Directory: "/work", Excluded: []string{"SC1091"}}
	for _, tc := range []struct {
		name      string
		version   int
		resources []analyzer.ToolResourceKind
		want      string
	}{
		{"check 1 carries no 0.3.0 members", 1, []analyzer.ToolResourceKind{analyzer.ToolDirectory}, `{"source":"echo","filename":"s.sh"}`},
		{"check 3 without the directory resource", 3, nil, `{"source":"echo","filename":"s.sh","excluded":["SC1091"]}`},
		{"check 3 with the directory resource", 3, []analyzer.ToolResourceKind{analyzer.ToolDirectory}, `{"source":"echo","filename":"s.sh","excluded":["SC1091"],"directory":"/work"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &Engine{checkVersion: tc.version, metadata: Metadata{Capabilities: Capabilities{Resources: tc.resources}}}
			got, err := e.encodeRequest(request)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	e := &Engine{checkVersion: 3}
	got, err := e.encodeRequest(analyzer.ScriptRequest{Source: "echo", Filename: "s.sh"})
	if err != nil || !strings.Contains(string(got), `"excluded":[]`) {
		t.Fatalf("check 3 sends an empty excluded list: %s %v", got, err)
	}
}

func TestCheckFindings3RejectsUndeclaredCapabilities(t *testing.T) {
	fix := []analyzer.ScriptFix{{Replacements: []analyzer.ScriptReplacement{{Row: 1, EndRow: 1, Column: 1, EndColumn: 2}}}}
	for _, tc := range []struct {
		name         string
		capabilities Capabilities
		finding      analyzer.ScriptDiagnostic
		want         string
	}{
		{"fixes", Capabilities{}, analyzer.ScriptDiagnostic{Fixes: fix}, "without the fixes capability"},
		{"source", Capabilities{Fixes: true}, analyzer.ScriptDiagnostic{Source: "lib.sh"}, "without the reported_sources capability"},
		{"fixes on a source", Capabilities{Fixes: true, ReportedSources: true}, analyzer.ScriptDiagnostic{Source: "lib.sh", Fixes: fix}, "fixes for reported source"},
		{"empty fix", Capabilities{Fixes: true}, analyzer.ScriptDiagnostic{Fixes: []analyzer.ScriptFix{{}}}, "without replacements"},
		{"reversed fix", Capabilities{Fixes: true}, analyzer.ScriptDiagnostic{Fixes: []analyzer.ScriptFix{{Replacements: []analyzer.ScriptReplacement{{Row: 2, EndRow: 1, Column: 1, EndColumn: 1}}}}}, "invalid range"},
		{"declared", Capabilities{Fixes: true, ReportedSources: true}, analyzer.ScriptDiagnostic{Fixes: fix}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &Engine{checkVersion: 3, metadata: Metadata{Capabilities: tc.capabilities}}
			got := e.checkFindings3(tc.finding)
			if tc.want == "" && got != "" || !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
