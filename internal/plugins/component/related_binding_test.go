package component

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/related"
	"actionlint.kjanat.dev/v2/source"
	"context"
	"errors"
	"github.com/kjanat/wacogo"
	"reflect"
	"strings"
	"testing"
)

type relatedProbe struct{ locations []diagnostic.Related }

func (relatedProbe) Descriptor() analyzer.Descriptor { return analyzer.Descriptor{Name: "example"} }
func (p relatedProbe) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return []diagnostic.Diagnostic{{Rule: "example", Message: "primary", Path: "workflow.yml", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 2}, Related: p.locations}}, nil
}

func wireRelatedDiagnostic(locations []diagnostic.Related) *wacogo.ValRecord {
	values := make([]wacogo.Val, 0, len(locations))
	for _, location := range locations {
		values = append(values, wacogo.NewValRecord(field("message", wacogo.ValString(location.Message)), field("path", wacogo.ValString(location.Path)), field("start", lowerPosition(location.Start)), field("end", lowerPosition(location.End))))
	}
	base := mixedDiagnostic("primary", diagnostic.Position{Line: 1, Column: 1}, diagnostic.Position{Line: 1, Column: 2})
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
	revision, err := source.NewRevision("/repo/workflow.yml", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	input := analyzer.Input{Path: "workflow.yml", Revision: revision, Coordinates: source.YAML}
	locations := []diagnostic.Related{{Message: "same request", Path: input.Path, Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 1}}, {Message: "foreign", Path: "other.yml", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 1}}}
	native, nativeErr := analyzerhost.Run(t.Context(), input, []analyzer.Registration{{Analyzer: relatedProbe{locations}}})
	wire, witErr := liftAnalysisForInput(wacogo.NewValRecord(field("diagnostics", wacogo.NewValListOf(wireRelatedDiagnostic(locations))), field("error", wacogo.ValOptionNone())), &input)
	a, na := errors.AsType[*related.RejectedError](nativeErr)
	b, nb := errors.AsType[*related.RejectedError](witErr)
	if !na || !nb || a.Count() != 1 || b.Count() != 1 || len(native) != 1 || len(wire) != 1 || !reflect.DeepEqual(native[0].Related, wire[0].Related) || native[0].Related[0].Revision != revision {
		t.Fatalf("native=%+v %v WIT=%+v %v", native, nativeErr, wire, witErr)
	}
}

func TestMalformedRelatedKeepsWITPrimaryAndCountsEveryLocation(t *testing.T) {
	locations := []diagnostic.Related{{Path: "workflow.yml", Start: diagnostic.Position{Line: 2, Column: 1}, End: diagnostic.Position{Line: 1, Column: 1}}, {Path: "workflow.yml", Start: diagnostic.Position{}, End: diagnostic.Position{Line: 1, Column: 1}}}
	findings, err := liftAnalysis(wacogo.NewValRecord(field("diagnostics", wacogo.NewValListOf(wireRelatedDiagnostic(locations))), field("error", wacogo.ValOptionNone())))
	rejected, ok := errors.AsType[*related.RejectedError](err)
	if !ok || rejected.Count() != 2 || len(findings) != 1 || findings[0].Message != "primary" || len(findings[0].Related) != 0 {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
}
