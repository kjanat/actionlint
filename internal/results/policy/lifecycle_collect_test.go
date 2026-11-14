package policy_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestCollectPreservesRequiredLifecycleProvenance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                               string
		declaration, requiredFirst         bool
		optionalSeverity, requiredSeverity string
	}{
		{"required invocation versus optional declaration", true, false, "warning", "error"},
		{"same severity optional declaration first", true, false, "warning", "warning"},
		{"same severity optional invocation first", false, false, "warning", "warning"},
		{"same severity required invocation first", false, true, "warning", "warning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			key := finding.MetadataKey{Path: "action.yml", Message: "runtime is deprecated"}
			optional := &finding.Finding{Rule: "action", Code: "runtime-deprecated", Path: "action.yml", Message: key.Message, Severity: tc.optionalSeverity, MetadataIdentity: &finding.MetadataIdentity{Key: key, Declaration: tc.declaration}}
			required := &finding.Finding{Rule: "action", Code: "runtime-deprecated", Path: "workflow.yml", Message: key.Message, Severity: tc.requiredSeverity, Lifecycle: &results.LifecycleReport{Policies: []results.LifecyclePolicyReport{{ID: "required", Enforced: true, Severity: tc.requiredSeverity}}}, RequiredLifecycle: true, MetadataIdentity: &finding.MetadataIdentity{Key: key}}
			files := []policy.File{{InputPath: "action.yml", Findings: []*finding.Finding{optional}}, {InputPath: "workflow.yml", Findings: []*finding.Finding{required}}}
			if tc.requiredFirst {
				files[0], files[1] = files[1], files[0]
			}
			diagnostics, _ := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect(files)
			found := false
			for _, d := range diagnostics {
				if d.Lifecycle != nil {
					for _, p := range d.Lifecycle.Policies {
						if p.ID == "required" && p.Enforced && d.Severity == tc.requiredSeverity && !d.NonBlocking {
							found = true
						}
					}
				}
			}
			if !found {
				t.Fatalf("required finding or its enforcement provenance was lost: %+v", diagnostics)
			}
		})
	}
}

func TestCollectKeepsDistinctRequiredPolicyOrigins(t *testing.T) {
	t.Parallel()
	key := finding.MetadataKey{Path: "action.yml", Message: "runtime is deprecated"}
	makeFinding := func(origin string) *finding.Finding {
		return &finding.Finding{Rule: "action", Code: "runtime-deprecated", Path: "workflow.yml", Message: key.Message, Severity: "warning", Lifecycle: &results.LifecycleReport{Policies: []results.LifecyclePolicyReport{{ID: "required", Origin: origin, Enforced: true, Severity: "warning"}}}, RequiredLifecycle: true, MetadataIdentity: &finding.MetadataIdentity{Key: key}}
	}
	files := []policy.File{{InputPath: "one.yml", Findings: []*finding.Finding{makeFinding("one.yml")}}, {InputPath: "two.yml", Findings: []*finding.Finding{makeFinding("two.yml")}}}
	diagnostics, _ := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect(files)
	seen := map[string]bool{}
	for _, d := range diagnostics {
		if d.Lifecycle != nil {
			for _, p := range d.Lifecycle.Policies {
				if p.Enforced {
					seen[p.Origin] = true
				}
			}
		}
	}
	if !seen["one.yml"] || !seen["two.yml"] {
		t.Fatalf("required policy origins lost: %+v", diagnostics)
	}
}
