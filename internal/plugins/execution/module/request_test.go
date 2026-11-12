package module

import (
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
)

func TestEncodeRequestPerCheckVersion(t *testing.T) {
	t.Parallel()
	request := checkinputs.ScriptRequest{Source: "echo", Filename: "s.sh", Directory: "/work", Excluded: []string{"SC1091"}}
	for _, tc := range []struct {
		name      string
		version   int
		resources []checkinputs.ToolResourceKind
		want      string
	}{
		{"check 1 carries no 0.3.0 members", 1, []checkinputs.ToolResourceKind{checkinputs.ToolDirectory}, `{"source":"echo","filename":"s.sh"}`},
		{"check 3 without the directory resource", 3, nil, `{"source":"echo","filename":"s.sh","excluded":["SC1091"]}`},
		{"check 3 with the directory resource", 3, []checkinputs.ToolResourceKind{checkinputs.ToolDirectory}, `{"source":"echo","filename":"s.sh","excluded":["SC1091"],"directory":"/work"}`},
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
	got, err := e.encodeRequest(checkinputs.ScriptRequest{Source: "echo", Filename: "s.sh"})
	if err != nil || !strings.Contains(string(got), `"excluded":[]`) {
		t.Fatalf("check 3 sends an empty excluded list: %s %v", got, err)
	}
}

func TestCheckFindings3RejectsUndeclaredCapabilities(t *testing.T) {
	t.Parallel()
	fix := []checkinputs.ScriptFix{{Replacements: []checkinputs.ScriptReplacement{{Row: 1, EndRow: 1, Column: 1, EndColumn: 2}}}}
	for _, tc := range []struct {
		name         string
		capabilities Capabilities
		finding      checkinputs.ScriptDiagnostic
		want         string
	}{
		{"absolute source", Capabilities{ReportedSources: true}, checkinputs.ScriptDiagnostic{Source: "/etc/passwd"}, "invalid source"},
		{"parent source", Capabilities{ReportedSources: true}, checkinputs.ScriptDiagnostic{Source: "../../secret"}, "invalid source"},
		{"backslash source", Capabilities{ReportedSources: true}, checkinputs.ScriptDiagnostic{Source: `scripts\lib.sh`}, "invalid source"},
		{"drive source", Capabilities{ReportedSources: true}, checkinputs.ScriptDiagnostic{Source: `C:\secret`}, "invalid source"},
		{"declared source", Capabilities{ReportedSources: true}, checkinputs.ScriptDiagnostic{Source: "scripts/lib.sh"}, ""},
		{"fixes", Capabilities{}, checkinputs.ScriptDiagnostic{Fixes: fix}, "without the fixes capability"},
		{"source", Capabilities{Fixes: true}, checkinputs.ScriptDiagnostic{Source: "lib.sh"}, "without the reported_sources capability"},
		{"fixes on a source", Capabilities{Fixes: true, ReportedSources: true}, checkinputs.ScriptDiagnostic{Source: "lib.sh", Fixes: fix}, "fixes for reported source"},
		{"empty fix", Capabilities{Fixes: true}, checkinputs.ScriptDiagnostic{Fixes: []checkinputs.ScriptFix{{}}}, "without replacements"},
		{"reversed fix", Capabilities{Fixes: true}, checkinputs.ScriptDiagnostic{Fixes: []checkinputs.ScriptFix{{Replacements: []checkinputs.ScriptReplacement{{Row: 2, EndRow: 1, Column: 1, EndColumn: 1}}}}}, "invalid range"},
		{"declared", Capabilities{Fixes: true, ReportedSources: true}, checkinputs.ScriptDiagnostic{Fixes: fix}, ""},
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
	if got := e.checkFindings3(checkinputs.ScriptDiagnostic{Source: "lib.sh"}, ""); !strings.Contains(got, "carried no directory") {
		t.Fatalf("source without a directory: %q", got)
	}
}
