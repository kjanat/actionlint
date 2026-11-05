package policy_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"

	"github.com/google/go-cmp/cmp"
)

func TestDisallowSuppressionsDiagnostic(t *testing.T) {
	const source = "name: café # actionlint:ignore cache-operation, cache-operation -- reviewed"
	cfg, err := checking.ConfigurationRules().Parse([]byte("policy: {disallow-suppressions: true}"))
	if err != nil {
		t.Fatal(err)
	}
	got := (policy.Processor{RuleSet: checking.ConfigurationRules()}).FilterInline([]byte(source), nil, cfg.Policy.DisallowSuppressions)
	if len(got) != 1 {
		t.Fatalf("expected one directive diagnostic, got %v", got)
	}
	want := &findingmodel.Finding{
		Start: diagmodel.Position{Line: 1, Column: 12}, Rule: "forbidden-suppression",
		Message: "inline suppression of cache-operation is disallowed by policy.forbidden-suppression",
	}
	if diff := cmp.Diff(want, got[0], cmp.AllowUnexported(findingmodel.Finding{})); diff != "" {
		t.Fatal(diff)
	}
}
