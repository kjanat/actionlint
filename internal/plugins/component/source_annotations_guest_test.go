package component_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/plugins/component"
	"actionlint.kjanat.dev/v2/source"
)

// Exercise the actual optional import through canonical lowering and lifting.
// The guest also probes an unauthorized revision before its legitimate queries.
func TestGuestReadsOnlyCurrentScriptAnnotations(t *testing.T) {
	guest := loadPlugin(t, "source-annotations", component.Options{})
	root := source.Reference{Document: "ci.yml", Revision: "workflow", Node: "step", Address: "jobs.test.steps[0]"}
	action := source.Reference{Document: "action.yml", Revision: "action", Node: "step", Address: "runs.steps[0]"}
	input := analyzer.Input{
		Path:    "ci.yml",
		Scripts: []analyzer.Script{{Reference: root, Text: "echo root", Comments: []analyzer.SourceComment{{Text: "# root", Start: diagnostic.Position{Line: 2, Column: 7}}}}},
		Invoked: []analyzer.Script{{Reference: action, Text: "echo action", Path: "action.yml", Comments: []analyzer.SourceComment{{Text: "# action", Start: diagnostic.Position{Line: 6, Column: 19}, Placement: analyzer.TrailingComment}}}},
	}
	findings, err := guest.Analyze(t.Context(), input)
	if err != nil || len(findings) != 2 {
		t.Fatalf("findings=%+v, error=%v", findings, err)
	}
	for i, want := range []struct {
		text, code, path string
		start            diagnostic.Position
		reference        source.Reference
	}{
		{"# root", "leading", "", diagnostic.Position{Line: 2, Column: 7}, root},
		{"# action", "trailing", "action.yml", diagnostic.Position{Line: 6, Column: 19}, action},
	} {
		finding := findings[i]
		if finding.Message != want.text || finding.Code != want.code || finding.Path != want.path || finding.Start != want.start || finding.End != want.start || finding.Reference == nil || *finding.Reference != want.reference {
			t.Fatalf("finding=%+v, want %+v", finding, want)
		}
	}
	input.Scripts[0].Comments = nil
	input.Invoked[0].Comments = nil
	if findings, err := guest.Analyze(t.Context(), input); err != nil || len(findings) != 0 {
		t.Fatalf("annotations leaked across guest instances: %+v, %v", findings, err)
	}
}

func TestGuestAnnotationFailureDoesNotPoisonLaterCalls(t *testing.T) {
	guest := loadPlugin(t, "source-annotations", component.Options{})
	ref := source.Reference{Document: "ci.yml", Revision: "workflow", Node: "step", Address: "jobs.test.steps[0]"}
	input := analyzer.Input{Scripts: []analyzer.Script{{Reference: ref, Text: "echo root", Comments: []analyzer.SourceComment{{Text: "# first\n# second", Start: diagnostic.Position{Line: 2, Column: 7}}}}}}
	if findings, err := guest.Analyze(t.Context(), input); findings != nil || err == nil || !strings.Contains(err.Error(), "invalid physical source comment at 2:7") {
		t.Fatalf("invalid comment did not fail through the guest import: findings=%+v, err=%v", findings, err)
	}
	input.Scripts[0].Comments[0].Text = "# recovered"
	findings, err := guest.Analyze(t.Context(), input)
	if err != nil || len(findings) != 1 || findings[0].Message != "# recovered" {
		t.Fatalf("annotation failure poisoned the next call: findings=%+v, err=%v", findings, err)
	}
}

func TestGuestAnnotationInterfaceVersions(t *testing.T) {
	data, err := os.ReadFile(pluginPath("source-annotations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0.1.1", "0.2.0"} {
		t.Run(version, func(t *testing.T) {
			// Keep encoded string lengths unchanged while changing the real import.
			name := "kjanat:actionlint/source-annotations@" + version
			changed := bytes.ReplaceAll(data, []byte(component.SourceAnnotationsInterface), []byte(name))
			if bytes.Equal(changed, data) {
				t.Fatal("fixture does not import the source annotations interface")
			}
			path := filepath.Join(t.TempDir(), "annotations.wasm")
			if err := os.WriteFile(path, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			guest, err := component.Load(t.Context(), path, component.Options{})
			if version == "0.2.0" {
				if guest != nil {
					_ = guest.Close(context.WithoutCancel(t.Context()))
				}
				want := "plugin imports " + name + "; this host provides " + component.SourceAnnotationsInterface
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("incompatible optional import admitted: err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("compatible patch version refused: %v", err)
			}
			t.Cleanup(func() { _ = guest.Close(context.WithoutCancel(t.Context())) })
			ref := source.Reference{Document: "ci.yml", Revision: "workflow", Node: "step", Address: "jobs.test.steps[0]"}
			findings, err := guest.Analyze(t.Context(), analyzer.Input{Scripts: []analyzer.Script{{Reference: ref, Comments: []analyzer.SourceComment{{Text: "# compatible", Start: diagnostic.Position{Line: 1, Column: 1}}}}}})
			if err != nil || len(findings) != 1 || findings[0].Message != "# compatible" {
				t.Fatalf("compatible import was not linked to the host: findings=%+v, err=%v", findings, err)
			}
		})
	}
}
