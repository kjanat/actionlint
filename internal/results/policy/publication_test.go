package policy_test

import (
	"bytes"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestPublicationDeduplicatesInferredAliasReferencesWithinEntry(t *testing.T) {
	t.Parallel()
	input := []*finding.Finding{
		{Rule: "expression-reference", Message: "same anchored expression", Start: sourcetext.Position{Line: 1, Column: 1}, InferredReference: true, Reference: &sourcetext.Reference{Document: "workflow", Revision: "revision", Node: "first invocation"}},
		{Rule: "expression-reference", Message: "same anchored expression", Start: sourcetext.Position{Line: 1, Column: 1}, InferredReference: true, Reference: &sourcetext.Reference{Document: "workflow", Revision: "revision", Node: "second invocation"}},
	}
	rules := checking.ConfigurationRules()
	p := preparePolicy(rules, nil, policy.DocumentInputs{Path: "ci.yml", Content: []byte("value")}, nil, input)
	result, err := policy.Publish([]policy.Batch{{Processor: policy.Processor{RuleSet: rules}, Policy: p, Findings: input}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("inferred alias owners produced duplicates: %+v", result.Diagnostics)
	}
	if input[0].Path != "" || input[1].Path != "" {
		t.Fatal("publication mutated its raw input records")
	}
}

func TestPublicationRejectsUnpreparedFindings(t *testing.T) {
	t.Parallel()
	input := []*finding.Finding{{Rule: "expression-reference", Message: "prepared"}}
	rules := checking.ConfigurationRules()
	p := preparePolicy(rules, nil, policy.DocumentInputs{Path: "valid.yml"}, nil, input)
	result, err := policy.Publish([]policy.Batch{
		{InputPath: "ci.yml", Findings: []*finding.Finding{{Rule: "action", Message: "unprepared"}}},
		{Processor: policy.Processor{RuleSet: rules}, Policy: p, Findings: input},
	}, nil)
	if err == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "prepared" {
		t.Fatalf("unprepared finding escaped publication: result=%+v error=%v", result, err)
	}
}

func TestPublicationCopiesEntryContent(t *testing.T) {
	t.Parallel()
	content := []byte("entry token\n")
	input := []*finding.Finding{{Rule: "expression-reference", Message: "finding", Start: sourcetext.Position{Line: 1, Column: 1}}}
	rules := checking.ConfigurationRules()
	p := preparePolicy(rules, nil, policy.DocumentInputs{Path: "ci.yml", Content: content}, nil, input)
	result, err := policy.Publish([]policy.Batch{{Processor: policy.Processor{RuleSet: rules}, Policy: p, Findings: input}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("missing source: %+v", result)
	}
	content[0] = 'X'
	if string(result.Diagnostics[0].Source.Content) != "entry token\n" {
		t.Fatal("publication reflects a later write to the caller's content")
	}
	result.Diagnostics[0].Source.Content[1] = 'Y'
	if content[1] != 'n' {
		t.Fatal("published source mutation escaped to the caller")
	}
}

func TestPublicationFailureInvalidatesUnusedSuppressionProof(t *testing.T) {
	t.Parallel()
	content := []byte("# actionlint-ignore-next-line expression-reference -- intentional\nrun: echo ok\n")
	ref := sourcetext.Reference{Document: "shared-action", Revision: sha256hex.Digest(content)}
	references := map[string]facts.SourceIndex{"action.yml": {Document: ref}}
	rules := checking.ConfigurationRules()
	p := preparePolicy(rules, nil, policy.DocumentInputs{Path: "action.yml", ConfigPath: "action.yml", Content: content}, nil, nil)
	result, err := policy.Publish([]policy.Batch{
		{Processor: policy.Processor{RuleSet: rules}, Policy: p, References: references},
		{InputPath: "failed.yml", References: references, Findings: []*finding.Finding{{Rule: "action", Message: "unprepared"}}},
	}, nil)
	if err == nil || len(result.Diagnostics) != 0 || len(result.IncompleteChecks) != 1 || result.IncompleteChecks[0].Source != ref {
		t.Fatalf("failed invocation allowed an unused-suppression claim: result=%+v error=%v", result, err)
	}
}

func TestPublicationCopiesForeignSourceOnceForOneHundredFindings(t *testing.T) {
	t.Parallel()
	content := bytes.Repeat([]byte("echo ok\n"), 8192)
	input := make([]*finding.Finding, 100)
	for i := range input {
		input[i] = &finding.Finding{Path: "shared/action.yml", Source: results.DiagnosticSource{Content: content}, Rule: "expression-reference", Message: "finding", Start: sourcetext.Position{Line: i + 1, Column: 1}}
	}
	rules := checking.ConfigurationRules()
	p := preparePolicy(rules, nil, policy.DocumentInputs{Path: "ci.yml", Content: []byte("entry\n")}, nil, input)
	result, err := policy.Publish([]policy.Batch{{Processor: policy.Processor{RuleSet: rules}, Policy: p, Findings: input}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != len(input) {
		t.Fatalf("retained %d findings, want %d", len(result.Diagnostics), len(input))
	}
	first := &result.Diagnostics[0].Source.Content[0]
	if first == &content[0] {
		t.Fatal("publication shares the caller's content array")
	}
	for _, reported := range result.Diagnostics {
		if len(reported.Source.Content) != 65536 || &reported.Source.Content[0] != first {
			t.Fatal("shared source was copied separately for each finding")
		}
	}
	content[0] = 'X'
	if result.Diagnostics[0].Source.Content[0] != 'e' {
		t.Fatal("caller mutation changed the published source")
	}
}
