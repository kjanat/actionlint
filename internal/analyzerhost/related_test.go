package analyzerhost

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/related"
	"actionlint.kjanat.dev/v2/source"
)

func TestRelatedRequestGrantUsesOnlySuppliedEvidence(t *testing.T) {
	revision, err := source.NewRevision("/repo/ci.yml", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, mixed := range []string{"valid", "revision only", "coordinates only", "invalid coordinates"} {
		t.Run(mixed, func(t *testing.T) {
			input := analyzer.Input{Path: "ci.yml", Revision: revision, Coordinates: source.YAML, Reference: source.Reference{Document: revision.Document(), Revision: revision.Digest()}}
			switch mixed {
			case "revision only":
				input.Coordinates = ""
			case "coordinates only":
				input.Revision = source.Revision{}
			case "invalid coordinates":
				input.Coordinates = "invalid"
			}
			locations := []diagnostic.Related{{Path: "ci.yml"}, {Path: "./ci.yml"}, {Path: "foreign.yml"}}
			registration := analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "related-probe"}, run: func(_ context.Context, supplied analyzer.Input) ([]diagnostic.Diagnostic, error) {
				if supplied.Revision != input.Revision || supplied.Coordinates != input.Coordinates {
					t.Fatal("request evidence projection changed")
				}
				supplied.Revision = source.Revision{}
				supplied.Coordinates = ""
				return []diagnostic.Diagnostic{{Message: "primary", Related: locations}}, nil
			}}}
			findings, err := Run(t.Context(), input, []analyzer.Registration{registration})
			want := 3
			if mixed == "valid" {
				want = 2
			}
			rejected, ok := errors.AsType[*related.RejectedError](err)
			if !ok || rejected.Count() != want || len(findings) != 1 || findings[0].Message != "primary" {
				t.Fatalf("findings=%+v err=%v", findings, err)
			}
			if mixed == "valid" && (len(findings[0].Related) != 1 || findings[0].Related[0].Revision != revision || findings[0].Related[0].Coordinates != source.YAML) {
				t.Fatal("exact request grant lost")
			}
			if locations[0].Revision.Valid() || locations[0].Coordinates != "" {
				t.Fatal("producer slice mutated")
			}
		})
	}
}
