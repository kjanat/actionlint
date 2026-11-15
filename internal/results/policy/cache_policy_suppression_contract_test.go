package policy_test

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"

	"github.com/google/go-cmp/cmp"
)

func TestCachePolicyInlineSuppressionKeepsOtherDiagnostics(t *testing.T) {
	t.Parallel()
	source := []byte("name: café # actionlint:ignore cache-operation -- reviewed\n")
	errs := []*findingmodel.Finding{
		{Start: sourcepos.Position{Line: 1}, Rule: "cache-operation"},
		{Start: sourcepos.Position{Line: 1}, Rule: "cache-operation", Source: []byte("other file")},
		{Start: sourcepos.Position{Line: 1}, Rule: "syntax-check"},
		{Start: sourcepos.Position{Line: 2}, Rule: "cache-operation"},
	}
	if diff := cmp.Diff(fixtureIdentities(errs[1:]), fixtureIdentities(fixtureInline(t, source, errs, nil))); diff != "" {
		t.Fatal(diff)
	}
	source = []byte("name: café # actionlint:ignore typo -- reason\n")
	got := fixtureInline(t, source, nil, nil)
	if len(got) != 1 || got[0].Start.Line != 1 || got[0].Start.Column != 12 {
		t.Fatalf("expected directive error at 1:12, got %v", got)
	}
}
