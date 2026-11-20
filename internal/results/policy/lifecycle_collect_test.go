package policy_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestPublicationPreservesRequiredLifecycleProvenance(t *testing.T) {
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
			optional := &finding.Finding{Rule: "action-runtime", Code: "runtime-deprecated", Path: "action.yml", Message: key.Message, Severity: severity.Severity(tc.optionalSeverity), MetadataIdentity: &finding.MetadataIdentity{Key: key, Declaration: tc.declaration}}
			required := &finding.Finding{Rule: "action-runtime", Code: "runtime-deprecated", Path: "workflow.yml", Message: key.Message, Severity: severity.Severity(tc.requiredSeverity), LifecycleEvidence: lifecycleFinding(t, "2026-11-01", true).LifecycleEvidence, MetadataIdentity: &finding.MetadataIdentity{Key: key}}
			files := []policy.Batch{{InputPath: "action.yml", Findings: []*finding.Finding{optional}}, {InputPath: "workflow.yml", Findings: []*finding.Finding{required}}}
			files[0] = prepareRetentionBatch(t, files[0], tc.optionalSeverity, "")
			files[1] = prepareRetentionBatch(t, files[1], tc.requiredSeverity, "trusted.yml")
			if tc.requiredFirst {
				files[0], files[1] = files[1], files[0]
			}
			published, err := policy.Publish(files, nil)
			if err != nil {
				t.Fatal(err)
			}
			diagnostics := published.Diagnostics
			found := false
			for _, d := range diagnostics {
				if d.Lifecycle != nil {
					for _, p := range d.Lifecycle.Policies {
						if p.ID == "required" && p.Enforced && string(d.Severity) == tc.requiredSeverity && !d.NonBlocking {
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

func TestPublicationKeepsDistinctRequiredPolicyOrigins(t *testing.T) {
	t.Parallel()
	key := finding.MetadataKey{Path: "action.yml", Message: "runtime is deprecated"}
	makeFinding := func() *finding.Finding {
		return &finding.Finding{Rule: "action-runtime", Code: "runtime-deprecated", Path: "workflow.yml", Message: key.Message, Severity: "warning", LifecycleEvidence: lifecycleFinding(t, "2026-11-01", true).LifecycleEvidence, MetadataIdentity: &finding.MetadataIdentity{Key: key}}
	}
	files := []policy.Batch{{InputPath: "one.yml", Findings: []*finding.Finding{makeFinding()}}, {InputPath: "two.yml", Findings: []*finding.Finding{makeFinding()}}}
	files[0] = prepareRetentionBatch(t, files[0], "warning", "one.yml")
	files[1] = prepareRetentionBatch(t, files[1], "warning", "two.yml")
	published, err := policy.Publish(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := published.Diagnostics
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

func prepareRetentionBatch(t *testing.T, batch policy.Batch, notified, origin string) policy.Batch {
	t.Helper()
	level := notified
	if notified == "warning" {
		level = "warn"
	}
	cfg := lifecycleRulesConfig(t, "lint: {rules: {correctness: {action-runtime: "+level+"}}}")
	batch.Processor = policy.Processor{RuleSet: checking.ConfigurationRules()}
	batch.Policy = preparePolicy(batch.Processor.RuleSet, cfg, policy.DocumentInputs{Path: batch.InputPath}, nil, batch.Findings)
	if origin != "" {
		required, err := lifecycle.NewPolicy("required", "Required migration", lifecycle.Match{Subjects: []lifecycle.Subject{lifecycle.Runners}, Events: []lifecycle.EventKind{lifecycle.SupportEnds}}, []lifecycle.Notification{{Starts: lifecycle.Before(180), Severity: severity.Severity(notified)}}, origin)
		if err != nil {
			t.Fatal(err)
		}
		batch.Policy.EnforcedLifecycle = []lifecycle.Policy{required}
	}
	return batch
}
