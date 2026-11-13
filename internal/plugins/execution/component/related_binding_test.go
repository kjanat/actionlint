package component

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	checkexecutor "actionlint.kjanat.dev/v2/internal/checking/executor"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/related"
	"github.com/kjanat/wacogo"
)

type relatedProbe struct{ locations []results.Related }

func (relatedProbe) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "example"}
}

func (p relatedProbe) CheckDocument(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
	return []results.Diagnostic{{Rule: "example", Message: "primary", Path: "workflow.yml", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 2}, Related: p.locations}}, nil
}

func wireRelatedDiagnostic(locations []results.Related) *wacogo.ValRecord {
	values := make([]wacogo.Val, 0, len(locations))
	for _, location := range locations {
		values = append(values, wacogo.NewValRecord(field("message", wacogo.ValString(location.Message)), field("path", wacogo.ValString(location.Path)), field("start", lowerPosition(location.Start)), field("end", lowerPosition(location.End))))
	}
	base := mixedDiagnostic("primary", sourcepos.Position{Line: 1, Column: 1}, sourcepos.Position{Line: 1, Column: 2})
	var fields []wacogo.Field
	for _, name := range []string{"reference", "related", "rule", "code", "severity", "message", "path", "explanation", "help", "documentation-url", "start", "end", "fixes"} {
		value := base.Field(name)
		if name == "related" {
			value = wacogo.NewValListOf(values...)
		}
		fields = append(fields, field(name, value))
	}
	return wacogo.NewValRecord(fields...)
}

func TestNativeAndWITRelatedRequestBindingAgree(t *testing.T) {
	t.Parallel()
	revision, err := sourcepos.NewRevision("/repo/workflow.yml", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	input := checkinputs.Input{Path: "workflow.yml", Revision: revision, Coordinates: sourcepos.YAML}
	locations := []results.Related{{Message: "same request", Path: input.Path, Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 1}}, {Message: "foreign", Path: "other.yml", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 1}}}
	native, nativeErr := checkexecutor.Run(t.Context(), input, []checkinputs.Registration{{Checker: relatedProbe{locations}}})
	wire, witErr := liftAnalysisForInput(wacogo.NewValRecord(field("diagnostics", wacogo.NewValListOf(wireRelatedDiagnostic(locations))), field("error", wacogo.ValOptionNone())), &input)
	a, na := errors.AsType[*related.RejectedError](nativeErr)
	b, nb := errors.AsType[*related.RejectedError](witErr)
	if !na || !nb || a.Count() != 1 || b.Count() != 1 || len(native) != 1 || len(wire) != 1 || !reflect.DeepEqual(native[0].Related, wire[0].Related) || native[0].Related[0].Revision != revision {
		t.Fatalf("native=%+v %v WIT=%+v %v", native, nativeErr, wire, witErr)
	}
}

func TestMalformedRelatedKeepsWITPrimaryAndCountsEveryLocation(t *testing.T) {
	t.Parallel()
	locations := []results.Related{{Path: "workflow.yml", Start: sourcepos.Position{Line: 2, Column: 1}, End: sourcepos.Position{Line: 1, Column: 1}}, {Path: "workflow.yml", Start: sourcepos.Position{}, End: sourcepos.Position{Line: 1, Column: 1}}}
	findings, err := liftAnalysis(wacogo.NewValRecord(field("diagnostics", wacogo.NewValListOf(wireRelatedDiagnostic(locations))), field("error", wacogo.ValOptionNone())))
	rejected, ok := errors.AsType[*related.RejectedError](err)
	if !ok || rejected.Count() != 2 || len(findings) != 1 || findings[0].Message != "primary" || len(findings[0].Related) != 0 {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
}
