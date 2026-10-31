package langserver

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/sourceid"
)

func TestDiagnosticRangeRejectsUnresolvableCoordinates(t *testing.T) {
	for _, start := range []diagnostic.Position{{Line: 2, Column: 1}, {Line: 0, Column: 1}, {Line: 1, Column: 9}} {
		if _, err := diagnosticRange([]byte("😀bad"), start, diagnostic.Position{}); err == nil {
			t.Fatalf("fabricated a range for %v", start)
		}
	}
	if _, err := diagnosticRange([]byte{0xff}, diagnostic.Position{Line: 1, Column: 1}, diagnostic.Position{}); err == nil {
		t.Fatal("invalid UTF-8 authorized coordinates")
	}
	start := diagnostic.Position{Line: 1, Column: 2}
	for _, end := range []diagnostic.Position{{}, {Line: 1, Column: 1}} {
		rangeValue, err := diagnosticRange([]byte("😀bad"), start, end)
		if err != nil || rangeValue.Start != (position{Line: 0, Character: 2}) || rangeValue.End != rangeValue.Start {
			t.Fatalf("legitimate point was lost: %+v %v", rangeValue, err)
		}
	}
}

func TestCodeActionSelectionUsesCapturedClosedDependency(t *testing.T) {
	root := t.TempDir()
	dependency, target := filepath.Join(root, "dependency.yml"), filepath.Join(root, "workflow.yml")
	content := "target"
	finding := diagnostic.Diagnostic{Path: dependency, Start: diagnostic.Position{Line: 1, Column: 2}, End: diagnostic.Position{Line: 1, Column: 5}, Fixes: []diagnostic.Fix{{Description: "update open caller", Applicability: "safe", Revisions: map[string]string{target: sourceid.Digest([]byte(content))}, Edits: []diagnostic.Edit{{Path: target, Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 2}, Replacement: "T"}}}}}
	sources := map[string]Document{dependency: {Text: "😀bad"}}
	open := map[string]Document{target: {URI: FileURI(target), Version: 3, Text: content}}
	actions := codeActions(t.Context(), dependency, sourceRange{Start: position{Character: 5}, End: position{Character: 5}}, []diagnostic.Diagnostic{finding}, sources, open)
	if len(actions) != 1 {
		t.Fatalf("closed dependency was ranged against a fabricated empty buffer: %+v", actions)
	}
	if actions := codeActions(t.Context(), dependency, sourceRange{}, []diagnostic.Diagnostic{finding}, nil, open); len(actions) != 0 {
		t.Fatal("uncaptured dependency produced a code action")
	}
}
