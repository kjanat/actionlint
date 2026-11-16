package policy_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results/finding"

	"github.com/google/go-cmp/cmp"
)

func TestDisallowSuppressionsDiagnostic(t *testing.T) {
	t.Parallel()
	const source = "name: café # actionlint:ignore cache-operation, cache-operation -- reviewed"
	cfg, err := checking.ConfigurationRules().ParseConfig([]byte("policy: {disallow-suppressions: true}"))
	if err != nil {
		t.Fatal(err)
	}
	got := fixtureInline(t, []byte(source), nil, cfg.Policy.DisallowSuppressions)
	if len(got) != 1 {
		t.Fatalf("expected one directive diagnostic, got %v", got)
	}
	want := &finding.Finding{
		Start: sourcepos.Position{Line: 1, Column: 12}, Rule: "forbidden-suppression",
		Message: "inline suppression of cache-operation is disallowed by policy.forbidden-suppression",
	}
	if diff := cmp.Diff(fixtureIdentities([]*finding.Finding{want}), fixtureIdentities(got)); diff != "" {
		t.Fatal(diff)
	}
}
