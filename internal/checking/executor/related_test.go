package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRelatedRequestGrantUsesOnlySuppliedEvidence(t *testing.T) {
	t.Parallel()
	revision, err := sourcetext.NewRevision("/repo/ci.yml", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, mixed := range []string{"valid", "revision only", "coordinates only", "invalid coordinates"} {
		t.Run(mixed, func(t *testing.T) {
			t.Parallel()
			input := checkinputs.AnalyzerInput{Path: "ci.yml", Revision: revision, Coordinates: sourcetext.YAML, Reference: sourcetext.Reference{Document: revision.Document(), Revision: revision.Digest()}}
			switch mixed {
			case "revision only":
				input.Coordinates = ""
			case "coordinates only":
				input.Revision = sourcetext.Revision{}
			case "invalid coordinates":
				input.Coordinates = "invalid"
			}
			locations := []results.Related{{Path: "ci.yml"}, {Path: "./ci.yml"}, {Path: "foreign.yml"}}
			registration := checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "related-probe"}}, run: func(_ context.Context, supplied checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
				if supplied.Revision != input.Revision || supplied.Coordinates != input.Coordinates {
					t.Fatal("request evidence projection changed")
				}
				supplied.Revision = sourcetext.Revision{}
				supplied.Coordinates = ""
				return []results.Diagnostic{{Message: "primary", Related: locations}}, nil
			}}}
			findings, err := Run(t.Context(), input, []checkinputs.Registration{registration})
			want := 3
			if mixed == "valid" {
				want = 2
			}
			rejected, ok := errors.AsType[*results.RejectedRelatedError](err)
			if !ok || rejected.Count() != want || len(findings) != 1 || findings[0].Message != "primary" {
				t.Fatalf("findings=%+v err=%v", findings, err)
			}
			if mixed == "valid" && (len(findings[0].Related) != 1 || findings[0].Related[0].Revision != revision || findings[0].Related[0].Coordinates != sourcetext.YAML) {
				t.Fatal("exact request grant lost")
			}
			if locations[0].Revision.Valid() || locations[0].Coordinates != "" {
				t.Fatal("producer slice mutated")
			}
		})
	}
}
