package module

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
)

func TestEncodeRequestPerCheckVersion(t *testing.T) {
	t.Parallel()
	request := inputs.ScriptRequest{Source: "echo", Filename: "s.sh", Directory: "/work", Excluded: []string{"SC1091"}}
	for _, tc := range []struct {
		name      string
		version   int
		resources []inputs.ToolResourceKind
		want      string
	}{
		{"check 1 carries no 0.3.0 members", 1, []inputs.ToolResourceKind{inputs.ToolDirectory}, `{"source":"echo","filename":"s.sh"}`},
		{"check 3 without the directory resource", 3, nil, `{"source":"echo","filename":"s.sh","excluded":["SC1091"]}`},
		{"check 3 with the directory resource", 3, []inputs.ToolResourceKind{inputs.ToolDirectory}, `{"source":"echo","filename":"s.sh","excluded":["SC1091"],"directory":"/work"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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
	got, err := e.encodeRequest(inputs.ScriptRequest{Source: "echo", Filename: "s.sh"})
	if err != nil || !strings.Contains(string(got), `"excluded":[]`) {
		t.Fatalf("check 3 sends an empty excluded list: %s %v", got, err)
	}
}

func TestCheckFindings3RejectsUndeclaredCapabilities(t *testing.T) {
	t.Parallel()
	fix := []inputs.ScriptFix{{Replacements: []inputs.ScriptReplacement{{Row: 1, EndRow: 1, Column: 1, EndColumn: 2}}}}
	for _, tc := range []struct {
		name         string
		capabilities Capabilities
		finding      inputs.ScriptDiagnostic
		want         string
	}{
		{"absolute source", Capabilities{ReportedSources: true}, inputs.ScriptDiagnostic{Source: "/etc/passwd"}, "invalid source"},
		{"parent source", Capabilities{ReportedSources: true}, inputs.ScriptDiagnostic{Source: "../../secret"}, "invalid source"},
		{"backslash source", Capabilities{ReportedSources: true}, inputs.ScriptDiagnostic{Source: `scripts\lib.sh`}, "invalid source"},
		{"drive source", Capabilities{ReportedSources: true}, inputs.ScriptDiagnostic{Source: `C:\secret`}, "invalid source"},
		{"declared source", Capabilities{ReportedSources: true}, inputs.ScriptDiagnostic{Source: "scripts/lib.sh"}, ""},
		{"fixes", Capabilities{}, inputs.ScriptDiagnostic{Fixes: fix}, "without the fixes capability"},
		{"source", Capabilities{Fixes: true}, inputs.ScriptDiagnostic{Source: "lib.sh"}, "without the reported_sources capability"},
		{"fixes on a source", Capabilities{Fixes: true, ReportedSources: true}, inputs.ScriptDiagnostic{Source: "lib.sh", Fixes: fix}, "fixes for reported source"},
		{"empty fix", Capabilities{Fixes: true}, inputs.ScriptDiagnostic{Fixes: []inputs.ScriptFix{{}}}, "without replacements"},
		{"reversed fix", Capabilities{Fixes: true}, inputs.ScriptDiagnostic{Fixes: []inputs.ScriptFix{{Replacements: []inputs.ScriptReplacement{{Row: 2, EndRow: 1, Column: 1, EndColumn: 1}}}}}, "invalid range"},
		{"declared", Capabilities{Fixes: true, ReportedSources: true}, inputs.ScriptDiagnostic{Fixes: fix}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := &Engine{checkVersion: 3, metadata: Metadata{Capabilities: tc.capabilities}}
			got := e.checkFindings3(tc.finding, "/work")
			if tc.want == "" && got != "" || !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	// A source anchors at the request directory; without one there is none.
	e := &Engine{checkVersion: 3, metadata: Metadata{Capabilities: Capabilities{ReportedSources: true}}}
	if got := e.checkFindings3(inputs.ScriptDiagnostic{Source: "lib.sh"}, ""); !strings.Contains(got, "carried no directory") {
		t.Fatalf("source without a directory: %q", got)
	}
}
