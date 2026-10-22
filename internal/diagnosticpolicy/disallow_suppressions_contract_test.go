package diagnosticpolicy_test

import (
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/diagnosticpolicy"
	findingmodel "actionlint.kjanat.dev/internal/finding"

	"github.com/google/go-cmp/cmp"
)

func TestDisallowSuppressionsDiagnostic(t *testing.T) {
	const source = "name: café # actionlint:ignore cache-operation, cache-operation -- reviewed"
	cfg, err := repoconfig.Parse([]byte("policy: {disallow-suppressions: true}"))
	if err != nil {
		t.Fatal(err)
	}
	got := diagnosticpolicy.FilterInline([]byte(source), nil, cfg.Policy.DisallowSuppressions)
	if len(got) != 1 {
		t.Fatalf("expected one directive diagnostic, got %v", got)
	}
	want := &findingmodel.Finding{
		Start: diagmodel.Position{Line: 1, Column: 12}, Rule: "disallow-suppressions",
		Message: "inline suppression of cache-operation is disallowed by policy.disallow-suppressions",
	}
	if diff := cmp.Diff(want, got[0], cmp.AllowUnexported(findingmodel.Finding{})); diff != "" {
		t.Fatal(diff)
	}
}
