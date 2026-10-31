package core_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"actionlint.kjanat.dev/v2/analyzer"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/core"
)

// An engine advertising checks 1 and 3 negotiates check 3, declares its
// capabilities and returns fixes and reported sources.
func TestCheck3Negotiation(t *testing.T) {
	engine := loadEngine(t, filepath.Join("testdata", "wat", "check-v3.wasm"), corewasm.Options{})
	if engine.CheckVersion() != 3 {
		t.Fatalf("negotiated check %d, want 3", engine.CheckVersion())
	}
	d := engine.ScriptDescriptor()
	if !d.Fixes || !d.ReportedSources || !slices.Equal(d.Resources, []analyzer.ToolResourceKind{analyzer.ToolDirectory}) {
		t.Fatalf("descriptor capabilities = %+v", d)
	}
	diagnostics, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "a\rbbbb\nccccc\n", Filename: "s.sh", Excluded: []string{"T9"}, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// The snippet's lone CR is a guest row; host rows are LF rows, and fix
	// replacements convert with their diagnostic.
	want := []analyzer.ScriptDiagnostic{
		{
			Filename: "s.sh", Code: "T1", Message: "fix me", Severity: "style",
			Location: analyzer.ScriptPosition{Row: 1, Column: 3}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 6},
			Fixes: []analyzer.ScriptFix{{Description: "replace", Replacements: []analyzer.ScriptReplacement{{Row: 1, EndRow: 1, Column: 3, EndColumn: 6, Text: "y"}}}},
		},
		{
			Filename: "lib.sh", Code: "T2", Message: "sourced", Severity: "info", Source: "lib.sh",
			Location: analyzer.ScriptPosition{Row: 3, Column: 2}, EndLocation: analyzer.ScriptPosition{Row: 3, Column: 5},
		},
	}
	if diff := cmp.Diff(want, diagnostics); diff != "" {
		t.Fatalf("diagnostics (-want +got):\n%s", diff)
	}
}

// An engine advertising only check 1 keeps working unchanged: check 3
// capabilities do not apply to it.
func TestCheck1EngineDeclaresNoCheck3Capabilities(t *testing.T) {
	engine := loadEngine(t, enginePath("echo"), corewasm.Options{})
	if engine.CheckVersion() != 1 {
		t.Fatalf("negotiated check %d, want 1", engine.CheckVersion())
	}
	if d := engine.ScriptDescriptor(); d.Fixes || d.ReportedSources || len(d.Resources) != 0 {
		t.Fatalf("descriptor capabilities = %+v", d)
	}
}
