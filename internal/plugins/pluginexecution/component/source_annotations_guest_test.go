package component_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
)

// Both the author example and host-boundary fixture use the real optional import.
// The fixture additionally queries an unauthorized revision.
func TestGuestReadsOnlyCurrentScriptAnnotations(t *testing.T) {
	t.Parallel()
	for _, fixture := range []string{"source-annotations", "source-annotations-example"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			checkGuestScriptAnnotations(t, fixture)
		})
	}
}

func checkGuestScriptAnnotations(t *testing.T, fixture string) {
	t.Helper()
	guest := loadPlugin(t, fixture, component.Options{})
	root := sourcetext.Reference{Document: "ci.yml", Revision: "workflow", Node: "step", Address: "jobs.test.steps[0]"}
	action := sourcetext.Reference{Document: "action.yml", Revision: "action", Node: "step", Address: "runs.steps[0]"}
	input := checkinputs.AnalyzerInput{
		AvailableInputs: []rule.InputKind{rule.Scripts},
		Path:            "ci.yml",
		Scripts:         []checkinputs.Script{{Reference: root, Text: "echo root", Comments: []facts.SourceComment{{Text: "# root", Start: sourcetext.Position{Line: 2, Column: 7}}}}},
		Invoked:         []checkinputs.Script{{Reference: action, Text: "echo action", Path: "action.yml", Comments: []facts.SourceComment{{Text: "# action", Start: sourcetext.Position{Line: 6, Column: 19}, Placement: facts.TrailingComment}}}},
	}
	findings, err := guest.CheckDocument(t.Context(), input)
	if err != nil || len(findings) != 2 {
		t.Fatalf("findings=%+v, error=%v", findings, err)
	}
	for i, want := range []struct {
		text, code, path string
		start            sourcetext.Position
		reference        sourcetext.Reference
	}{
		{"# root", "leading", "", sourcetext.Position{Line: 2, Column: 7}, root},
		{"# action", "trailing", "action.yml", sourcetext.Position{Line: 6, Column: 19}, action},
	} {
		finding := findings[i]
		if finding.Message != want.text || finding.Code != want.code || finding.Path != want.path || finding.Start != want.start || finding.End != want.start || finding.Reference == nil || *finding.Reference != want.reference {
			t.Fatalf("finding=%+v, want %+v", finding, want)
		}
	}
	input.Scripts[0].Comments = nil
	input.Invoked[0].Comments = nil
	if findings, err := guest.CheckDocument(t.Context(), input); err != nil || len(findings) != 0 {
		t.Fatalf("annotations leaked across guest instances: %+v, %v", findings, err)
	}
}

func TestGuestAnnotationFailureDoesNotPoisonLaterCalls(t *testing.T) {
	t.Parallel()
	guest := loadPlugin(t, "source-annotations", component.Options{})
	ref := sourcetext.Reference{Document: "ci.yml", Revision: "workflow", Node: "step", Address: "jobs.test.steps[0]"}
	input := checkinputs.AnalyzerInput{AvailableInputs: []rule.InputKind{rule.Scripts}, Scripts: []checkinputs.Script{{Reference: ref, Text: "echo root", Comments: []facts.SourceComment{{Text: "# first\n# second", Start: sourcetext.Position{Line: 2, Column: 7}}}}}}
	if findings, err := guest.CheckDocument(t.Context(), input); findings != nil || err == nil || !strings.Contains(err.Error(), "invalid physical source comment at 2:7") {
		t.Fatalf("invalid comment did not fail through the guest import: findings=%+v, err=%v", findings, err)
	}
	input.Scripts[0].Comments[0].Text = "# recovered"
	findings, err := guest.CheckDocument(t.Context(), input)
	if err != nil || len(findings) != 1 || findings[0].Message != "# recovered" {
		t.Fatalf("annotation failure poisoned the next call: findings=%+v, err=%v", findings, err)
	}
}

func TestGuestAnnotationInterfaceVersions(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(pluginPath("source-annotations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0.2.1", "0.3.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
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
			guest, err := component.LoadComponentPlugin(t.Context(), path, component.Options{})
			if version == "0.3.0" {
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
			ref := sourcetext.Reference{Document: "ci.yml", Revision: "workflow", Node: "step", Address: "jobs.test.steps[0]"}
			findings, err := guest.CheckDocument(t.Context(), checkinputs.AnalyzerInput{AvailableInputs: []rule.InputKind{rule.Scripts}, Scripts: []checkinputs.Script{{Reference: ref, Comments: []facts.SourceComment{{Text: "# compatible", Start: sourcetext.Position{Line: 1, Column: 1}}}}}})
			if err != nil || len(findings) != 1 || findings[0].Message != "# compatible" {
				t.Fatalf("compatible import was not linked to the host: findings=%+v, err=%v", findings, err)
			}
		})
	}
}
