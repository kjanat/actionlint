package define

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestDeclarationAdapterPreservesInternalRecord(t *testing.T) {
	t.Parallel()
	declaration := rule.Declaration{Name: "native-record", Category: "correctness"}
	rules, err := configuration.NewRuleSet([]rule.Declaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	want := finding.Finding{
		Rule: "native-record", Code: "example", Message: "Example finding", Severity: "warning",
		Path: "script.sh", Start: sourcetext.Position{Line: 2, Column: 3},
		LifecycleEvidence: &lifecycle.Evidence{Subject: lifecycle.LanguageRuntimes, Name: "example"},
		End:               sourcetext.Position{Line: 2, Column: 9},
		Source:            results.DiagnosticSource{Content: []byte("first\nsecond\n"), RawScript: true},
		MetadataIdentity:  &finding.MetadataIdentity{Key: finding.MetadataKey{Path: "action.yml", Message: "Example finding"}, Visit: 7},
	}
	adapter := NewChecksVisitor(rules, declaration.Name, Checks{
		Workflow: func(workflows.BoundWorkflow) []reporting.StepFinding { return []reporting.StepFinding{{Finding: want}} },
	})
	if err := adapter.VisitWorkflowPre(workflows.BoundWorkflow{}); err != nil {
		t.Fatal(err)
	}
	got := adapter.Errs()
	if len(got) != 1 {
		t.Fatalf("expected one forwarded finding, got %d", len(got))
	}
	if !reflect.DeepEqual(*got[0], want) {
		t.Fatalf("declaration adapter changed the internal record:\n got: %+v\nwant: %+v", *got[0], want)
	}
}
