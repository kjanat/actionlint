package module

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

func TestEncodeRequestPerCheckVersion(t *testing.T) {
	t.Parallel()
	request := scriptengine.Request{Source: "echo", Filename: "s.sh", Directory: "/work", Excluded: []string{"SC1091"}}
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
	got, err := e.encodeRequest(scriptengine.Request{Source: "echo", Filename: "s.sh"})
	if err != nil || !strings.Contains(string(got), `"excluded":[]`) {
		t.Fatalf("check 3 sends an empty excluded list: %s %v", got, err)
	}
}

func TestCheckFindings3RejectsUndeclaredCapabilities(t *testing.T) {
	t.Parallel()
	fix := []scriptengine.Fix{{Replacements: []scriptengine.Replacement{{Start: sourcetext.Position{Line: 1, Column: 1}, End: sourcetext.Position{Line: 1, Column: 2}}}}}
	for _, tc := range []struct {
		name         string
		capabilities Capabilities
		finding      scriptengine.Diagnostic
		want         string
	}{
		{"absolute source", Capabilities{ReportedSources: true}, scriptengine.Diagnostic{Source: "/etc/passwd"}, "invalid source"},
		{"parent source", Capabilities{ReportedSources: true}, scriptengine.Diagnostic{Source: "../../secret"}, "invalid source"},
		{"backslash source", Capabilities{ReportedSources: true}, scriptengine.Diagnostic{Source: `scripts\lib.sh`}, "invalid source"},
		{"drive source", Capabilities{ReportedSources: true}, scriptengine.Diagnostic{Source: `C:\secret`}, "invalid source"},
		{"declared source", Capabilities{ReportedSources: true}, scriptengine.Diagnostic{Source: "scripts/lib.sh"}, ""},
		{"fixes", Capabilities{}, scriptengine.Diagnostic{Fixes: fix}, "without the fixes capability"},
		{"source", Capabilities{Fixes: true}, scriptengine.Diagnostic{Source: "lib.sh"}, "without the reported_sources capability"},
		{"fixes on a source", Capabilities{Fixes: true, ReportedSources: true}, scriptengine.Diagnostic{Source: "lib.sh", Fixes: fix}, "fixes for reported source"},
		{"declared", Capabilities{Fixes: true, ReportedSources: true}, scriptengine.Diagnostic{Fixes: fix}, ""},
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
	// A source anchors at the request directory. Without a request directory, a source has no anchor.
	e := &Engine{checkVersion: 3, metadata: Metadata{Capabilities: Capabilities{ReportedSources: true}}}
	if got := e.checkFindings3(scriptengine.Diagnostic{Source: "lib.sh"}, ""); !strings.Contains(got, "carried no directory") {
		t.Fatalf("source without a directory: %q", got)
	}
}
