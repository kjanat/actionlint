package lsp

import (
	"path/filepath"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestDiagnosticRangeRejectsUnresolvableCoordinates(t *testing.T) {
	t.Parallel()
	for _, start := range []sourcepos.Position{{Line: 2, Column: 1}, {Line: 0, Column: 1}, {Line: 1, Column: 9}} {
		if _, err := diagnosticRange([]byte("😀bad"), start, sourcepos.Position{}); err == nil {
			t.Fatalf("fabricated a range for %v", start)
		}
	}
	if _, err := diagnosticRange([]byte{0xff}, sourcepos.Position{Line: 1, Column: 1}, sourcepos.Position{}); err == nil {
		t.Fatal("invalid UTF-8 authorized coordinates")
	}
	start := sourcepos.Position{Line: 1, Column: 2}
	for _, end := range []sourcepos.Position{{}, {Line: 1, Column: 1}} {
		rangeValue, err := diagnosticRange([]byte("😀bad"), start, end)
		if err != nil || rangeValue.Start != (position{Line: 0, Character: 2}) || rangeValue.End != rangeValue.Start {
			t.Fatalf("legitimate point was lost: %+v %v", rangeValue, err)
		}
	}
}

func TestCodeActionSelectionUsesCapturedClosedDependency(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dependency, target := filepath.Join(root, "dependency.yml"), filepath.Join(root, "workflow.yml")
	content := "target"
	finding := results.Diagnostic{Path: dependency, Start: sourcepos.Position{Line: 1, Column: 2}, End: sourcepos.Position{Line: 1, Column: 5}, Fixes: []results.Fix{{Description: "update open caller", Applicability: results.FixSafe, Revisions: map[string]string{target: sourceid.Digest([]byte(content))}, Edits: []results.Edit{{Path: target, Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 2}, Replacement: "T"}}}}}
	sources := map[string]Document{dependency: {Text: "😀bad"}}
	open := map[string]Document{target: {URI: FileURI(target), Version: 3, Text: content}}
	actions := codeActions(t.Context(), dependency, sourceRange{Start: position{Character: 5}, End: position{Character: 5}}, []results.Diagnostic{finding}, sources, open)
	if len(actions) != 1 {
		t.Fatalf("closed dependency was ranged against a fabricated empty buffer: %+v", actions)
	}
	if actions := codeActions(t.Context(), dependency, sourceRange{}, []results.Diagnostic{finding}, nil, open); len(actions) != 0 {
		t.Fatal("uncaptured dependency produced a code action")
	}
}

func TestSuppressionActionsDeduplicateAndRejectStaleBuffers(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workflow.yml")
	const text = "run: echo ok\n"
	doc := Document{URI: FileURI(path), Version: 8, Text: text}
	point := sourcepos.Position{Line: 1, Column: 1}
	fix := results.Fix{Description: "Suppress shellcheck for this declaration", Applicability: results.FixSuggested, Revisions: map[string]string{path: sourceid.Digest([]byte(text))}, Edits: []results.Edit{{Path: path, Start: point, End: point, Replacement: "# actionlint-ignore-next-line shellcheck\n"}}}
	findings := []results.Diagnostic{
		{Path: path, Rule: "shellcheck", Code: "SC2086", Start: point, Fixes: []results.Fix{fix}},
		{Path: path, Rule: "shellcheck", Code: "SC2154", Start: point, Fixes: []results.Fix{fix}},
	}
	sources, open := map[string]Document{path: doc}, map[string]Document{path: doc}
	if actions := codeActions(t.Context(), path, sourceRange{}, findings, sources, open); len(actions) != 1 {
		t.Fatalf("identical suppression edits should appear once: %+v", actions)
	}
	doc.Text, doc.Version = "run: echo changed\n", 9
	open[path] = doc
	if actions := codeActions(t.Context(), path, sourceRange{}, findings, sources, open); len(actions) != 0 {
		t.Fatalf("stale suppression authorized an edit: %+v", actions)
	}
}

func TestCodeActionsUseEachFindingsFixes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workflow.yml")
	const text = "first\nsecond\nthird\n"
	doc := Document{URI: FileURI(path), Version: 1, Text: text}
	documents := map[string]Document{path: doc}
	var findings []results.Diagnostic
	for i, applicability := range []results.Applicability{results.FixSuggested, results.FixSuggested, results.FixSafe} {
		point := sourcepos.Position{Line: i + 1, Column: 1}
		finding := results.Diagnostic{Path: path, Rule: "same-rule", Start: point}
		if i != 0 {
			finding.Fixes = []results.Fix{{
				Description: "replace " + applicability.String(), Applicability: applicability,
				Revisions: map[string]string{path: sourceid.Digest([]byte(text))},
				Edits:     []results.Edit{{Path: path, Start: point, End: point, Replacement: "fixed "}},
			}}
		}
		findings = append(findings, finding)
	}
	for i := range findings {
		selection := sourceRange{Start: position{Line: i}, End: position{Line: i}}
		actions := codeActions(t.Context(), path, selection, findings, documents, documents)
		if i == 0 {
			if len(actions) != 0 {
				t.Fatalf("finding without edits offered a fix: %+v", actions)
			}
			continue
		}
		if len(actions) != 1 {
			t.Fatalf("finding %d: got %d actions", i, len(actions))
		}
		action, ok := actions[0].(map[string]any)
		if !ok || action["isPreferred"] != (i == 2) {
			t.Fatalf("finding %d: incorrect applicability: %+v", i, actions)
		}
	}
}
