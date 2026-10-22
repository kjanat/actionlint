package diagnosticpolicy_test

import (
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/diagnosticpolicy"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"

	"github.com/google/go-cmp/cmp"
)

func TestCachePolicyInlineSuppressionKeepsOtherDiagnostics(t *testing.T) {
	source := []byte("name: café # actionlint:ignore cache-operation -- reviewed\n")
	errs := []*findingmodel.Finding{
		{Start: diagmodel.Position{Line: 1}, Rule: "cache-operation"},
		{Start: diagmodel.Position{Line: 1}, Rule: "cache-operation", Source: []byte("other file")},
		{Start: diagmodel.Position{Line: 1}, Rule: "syntax-check"},
		{Start: diagmodel.Position{Line: 2}, Rule: "cache-operation"},
	}
	if diff := cmp.Diff(errs[1:], diagnosticpolicy.FilterInline(source, errs, nil), cmp.AllowUnexported(findingmodel.Finding{})); diff != "" {
		t.Fatal(diff)
	}
	source = []byte("name: café # actionlint:ignore typo -- reason\n")
	got := diagnosticpolicy.FilterInline(source, nil, nil)
	if len(got) != 1 || got[0].Start.Line != 1 || got[0].Start.Column != 12 {
		t.Fatalf("expected directive error at 1:12, got %v", got)
	}
}
