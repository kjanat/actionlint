package documentparse

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	documentmodel "actionlint.kjanat.dev/v2/document"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestActionAdmissionRejectsUnvisitedExpansionBeforeRecovery(t *testing.T) {
	for _, brokenSchema := range []bool{false, true} {
		t.Run(strconv.FormatBool(brokenSchema), func(t *testing.T) {
			var source strings.Builder
			source.WriteString("name: recovered\ndescription: test\n")
			if brokenSchema {
				source.WriteString("inputs: invalid\n")
			}
			source.WriteString("runs:\n  using: composite\n  steps:\n    - run: echo recovered\n      shell: bash\n      a0: &a0 [value]\n")
			for level := 1; level <= 8; level++ {
				fmt.Fprintf(&source, "      a%d: &a%d [*a%d, *a%d, *a%d]\n", level, level, level-1, level-1, level-1)
			}
			content := []byte(source.String())
			metadata, err := actionmeta.Parse("action.yml", content)
			if _, ok := errors.AsType[*codec.AdmissionError](err); !ok || !strings.Contains(err.Error(), "excessive aliasing") {
				t.Fatalf("unvisited expansion bypassed action admission: %v", err)
			}
			if metadata.Name != "recovered" || metadata.Runs.Using != "composite" || len(metadata.Runs.Steps) != 1 {
				t.Fatal("admission discarded decoded declarations")
			}
			document := Action("action.yml", "action.yml", content)
			if document.CanAnalyze() || document.Provenance.record != nil && document.Provenance.record.Bind("rejected-action").ActionBody().Present() {
				t.Fatal("rejected action supplied semantic input")
			}
			for _, documentFirst := range []bool{false, true} {
				snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
				bind := func(fromDocument bool) (SourceSnapshot, error) {
					if fromDocument {
						return snapshots.Document("action.yml", content, document)
					}
					return snapshots.Action("action.yml", metadata)
				}
				first, firstErr := bind(documentFirst)
				second, secondErr := bind(!documentFirst)
				if firstErr != nil || secondErr != nil || !first.Present() || first != second {
					t.Fatalf("source-only snapshot depends on entry order: first=%p/%v second=%p/%v", first, firstErr, second, secondErr)
				}
				if body, ok := first.ActionView(); ok {
					for range body.Steps() {
						t.Fatal("rejected action constructed frozen semantic provenance")
					}
				}
			}
			outline, ok := document.Outline.(documentmodel.Action)
			if !ok || outline.Name() != "recovered" || outline.ParseStatus() != "partial" {
				t.Fatalf("recovered outline lost: %+v", document.Outline)
			}
			var budget, schema bool
			for _, finding := range document.Findings {
				budget = budget || strings.Contains(finding.Message, "excessive aliasing")
				schema = schema || strings.Contains(finding.Message, "inputs must be mapping")
			}
			if !budget || brokenSchema && !schema {
				t.Fatalf("admission or schema findings lost: %v", document.Findings)
			}
		})
	}
}

func TestActionAdmissionKeepsOriginalFatalDecodeFinding(t *testing.T) {
	var source strings.Builder
	source.WriteString("name: recovered\ndefinitions: &definitions\n")
	for input := range 650 {
		fmt.Fprintf(&source, "  input%d: {description: value}\n", input)
	}
	source.WriteString("inputs: *definitions\nruns: {using: composite, steps: []}\n")
	content := []byte(source.String())
	root, err := codec.Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	var baseline actionmeta.Metadata
	want := actionmeta.Decode(root, &baseline)
	if _, fatal := codec.FatalLoadError(want); !fatal || !strings.Contains(want.Error(), "excessive aliasing") {
		t.Fatalf("fixture must fail selected decoding: %v", want)
	}
	_, got := actionmeta.Parse("action.yml", content)
	_, rejected := errors.AsType[*codec.AdmissionError](got)
	if rejected != (workflowparser.AdmitYAMLAliases(root) != nil) || got.Error() != want.Error() {
		t.Fatalf("admission changed the original fatal error: got %v, want %v", got, want)
	}
	document := Action("action.yml", "action.yml", content)
	if document.CanAnalyze() || len(document.Findings) != 1 || !strings.Contains(document.Findings[0].Message, "excessive aliasing") {
		t.Fatalf("fatal decoding produced duplicate findings or semantic facts: %v", document.Findings)
	}
}

func TestActionAdmissionRejectsParallelExpansion(t *testing.T) {
	var source strings.Builder
	source.WriteString("name: parallel\nruns:\n  using: composite\n  steps:\n    - a0: &a0 [{run: echo ok, shell: bash}]\n")
	for level := 1; level <= 8; level++ {
		fmt.Fprintf(&source, "      a%d: &a%d [{parallel: *a%d}, {parallel: *a%d}, {parallel: *a%d}]\n", level, level, level-1, level-1, level-1)
	}
	source.WriteString("      parallel: *a8\n")
	document := Action("action.yml", "action.yml", []byte(source.String()))
	if _, rejected := errors.AsType[*codec.AdmissionError](document.ParseError); !rejected || document.CanAnalyze() || document.Provenance.record != nil && document.Provenance.record.Bind("rejected-action").ActionBody().Present() {
		t.Fatal("parallel expansion bypassed action admission", document.ParseError)
	}
	if findings := actionmeta.Validate(document.Metadata); len(findings) == 0 {
		t.Fatal("source-only schema validation lost physical alias findings")
	}
}

func TestActionAdmissionKeepsRecursiveStepsOpaque(t *testing.T) {
	const source = "name: cycle\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n      unknown: &cycle [*cycle]\n"
	document := Action("action.yml", "action.yml", []byte(source))
	if document.ParseError != nil || !document.CanAnalyze() {
		t.Fatal("recursive recovery became an admission failure", document.ParseError)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("action.yml", []byte(source), document)
	if err != nil {
		t.Fatal(err)
	}
	steps := snapshotActionSteps(t, snapshot)
	if len(steps) != 1 {
		t.Fatal("recursive step lost its opaque occurrence")
	}
	if _, opaque := steps[0].Action(); !opaque {
		t.Fatal("recursive composite step became executable")
	}
}

func TestActionAdmissionPreservesEmptyInputs(t *testing.T) {
	for _, source := range []string{"", " ", "# comment\n"} {
		metadata, err := actionmeta.Parse("action.yml", []byte(source))
		if err != nil || metadata == nil || metadata.Root != nil {
			t.Fatalf("empty manifest changed: %q: metadata=%v error=%v", source, metadata, err)
		}
	}
}

func TestRecoveredActionSnapshotsHaveStableEligibility(t *testing.T) {
	for _, header := range []string{"inputs: invalid\n", ""} {
		content := []byte("name: recovered\n" + header + "runs: &runs\n  using: composite\n  steps: [{run: echo recovered, shell: bash}]\n  env: *runs\n")
		metadata, parseErr := actionmeta.Parse("action.yml", content)
		if parseErr == nil || !strings.Contains(parseErr.Error(), "contains itself") {
			t.Fatal("fixture must trigger a fatal recursive selected decode", parseErr)
		}
		if _, rejected := errors.AsType[*codec.AdmissionError](parseErr); rejected {
			t.Fatal("recoverable recursive provenance became resource rejection")
		}
		document := Action("action.yml", "action.yml", content)
		if document.CanAnalyze() || len(document.Findings) != 1 {
			t.Fatal("parse failure became executable or duplicated", document.Findings)
		}
		for _, documentFirst := range []bool{false, true} {
			snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			bind := func(fromDocument bool) (SourceSnapshot, error) {
				if fromDocument {
					return snapshots.Document("action.yml", content, document)
				}
				return snapshots.Action("action.yml", metadata)
			}
			first, firstErr := bind(documentFirst)
			second, secondErr := bind(!documentFirst)
			if firstErr != nil || secondErr != nil || !first.Present() || first != second {
				t.Fatal("recovered snapshot depends on entry order", firstErr, secondErr)
			}
			steps := snapshotActionSteps(t, first)
			if len(steps) != 1 {
				t.Fatal("recovered declarations lost canonical step eligibility")
			}
		}
	}
}

func TestSchemaErrorActionSnapshotsHaveStableDeclarations(t *testing.T) {
	content := []byte("name: recovered\ninputs: invalid\nruns: {using: composite, steps: [{run: echo ok, shell: bash}]}\n")
	metadata, parseErr := actionmeta.Parse("action.yml", content)
	if parseErr == nil {
		t.Fatal("fixture must recover declarations alongside a schema error")
	}
	document := Action("action.yml", "action.yml", content)
	if document.CanAnalyze() {
		t.Fatal("source recovery enabled failed analysis")
	}
	for _, documentFirst := range []bool{false, true} {
		snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
		bind := func(fromDocument bool) (SourceSnapshot, error) {
			if fromDocument {
				return snapshots.Document("action.yml", content, document)
			}
			return snapshots.Action("action.yml", metadata)
		}
		first, firstErr := bind(documentFirst)
		second, secondErr := bind(!documentFirst)
		if firstErr != nil || secondErr != nil || !first.Present() || first != second {
			t.Fatal("schema-error declaration binding depends on entry order", firstErr, secondErr)
		}
		steps := snapshotActionSteps(t, first)
		if len(steps) != 1 {
			t.Fatal("schema-error declarations disappeared from the source snapshot")
		}
	}
}

func TestFatalDecodePrecedesAnIndependentAdmissionFailure(t *testing.T) {
	var source strings.Builder
	source.WriteString("name: rejected\nruns: &runs\n  using: composite\n  env: *runs\n  steps:\n    - run: echo ok\n      shell: bash\n      a0: &a0 [value]\n")
	for level := 1; level <= 8; level++ {
		fmt.Fprintf(&source, "      a%d: &a%d [*a%d, *a%d, *a%d]\n", level, level, level-1, level-1, level-1)
	}
	document := Action("action.yml", "action.yml", []byte(source.String()))
	if _, rejected := errors.AsType[*codec.AdmissionError](document.ParseError); !rejected || document.CanAnalyze() {
		t.Fatal("independent resource rejection did not gate analysis", document.ParseError)
	}
	if len(document.Findings) != 1 || !strings.Contains(document.Findings[0].Message, "contains itself") {
		t.Fatal("first fatal decode lost diagnostic precedence", document.Findings)
	}
}
