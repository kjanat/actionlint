package module_test

import (
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/module"
	"github.com/google/go-cmp/cmp"
)

func TestCheck3Negotiation(t *testing.T) {
	t.Parallel()
	engine := loadEngine(t, filepath.Join("testdata", "wat", "check-v3.wasm"), module.Options{})
	if engine.CheckVersion() != 3 {
		t.Fatalf("negotiated check %d, want 3", engine.CheckVersion())
	}
	d := engine.ScriptDescriptor()
	if !d.Fixes || !d.ReportedSources || !slices.Equal(d.Resources, []checkinputs.ToolResourceKind{checkinputs.ToolDirectory}) {
		t.Fatalf("descriptor capabilities = %+v", d)
	}
	diagnostics, err := engine.Check(t.Context(), scriptengine.Request{Source: "a\rbbbb\nccccc\n", Filename: "s.sh", Excluded: []string{"T9"}, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// The snippet's lone CR is a guest row; host rows are LF rows, and fix
	// replacements convert with their diagnostic.
	want := []scriptengine.Diagnostic{
		{
			Filename: "s.sh", Code: "T1", Message: "fix me", Severity: "style",
			Location: sourcetext.Position{Line: 1, Column: 3}, EndLocation: sourcetext.Position{Line: 1, Column: 6},
			Fixes: []scriptengine.Fix{{Description: "replace", Replacements: []scriptengine.Replacement{{Start: sourcetext.Position{Line: 1, Column: 3}, End: sourcetext.Position{Line: 1, Column: 6}, Text: "y"}}}},
		},
		{
			Filename: "lib.sh", Code: "T2", Message: "sourced", Severity: "info", Source: "lib.sh",
			Location: sourcetext.Position{Line: 3, Column: 2}, EndLocation: sourcetext.Position{Line: 3, Column: 5},
		},
	}
	if diff := cmp.Diff(want, diagnostics); diff != "" {
		t.Fatalf("diagnostics (-want +got):\n%s", diff)
	}
}

func TestCheck1EngineDeclaresNoCheck3Capabilities(t *testing.T) {
	t.Parallel()
	engine := loadEngine(t, enginePath("echo"), module.Options{})
	if engine.CheckVersion() != 1 {
		t.Fatalf("negotiated check %d, want 1", engine.CheckVersion())
	}
	if d := engine.ScriptDescriptor(); d.Fixes || d.ReportedSources || len(d.Resources) != 0 {
		t.Fatalf("descriptor capabilities = %+v", d)
	}
}
