package policy_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"

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
	if diff := cmp.Diff(errs[1:], (policy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline(source, errs, nil), cmp.AllowUnexported(findingmodel.Finding{})); diff != "" {
		t.Fatal(diff)
	}
	source = []byte("name: café # actionlint:ignore typo -- reason\n")
	got := (policy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline(source, nil, nil)
	if len(got) != 1 || got[0].Start.Line != 1 || got[0].Start.Column != 12 {
		t.Fatalf("expected directive error at 1:12, got %v", got)
	}
}
