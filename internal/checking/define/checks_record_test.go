package define

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestDeclarationAdapterPreservesInternalRecord(t *testing.T) {
	t.Parallel()
	declaration := rule.Declaration{Name: "native-record", Category: "correctness"}
	rules, err := configuration.NewRuleSet([]rule.Declaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	want := findingmodel.Finding{
		Rule: "native-record", Code: "example", Message: "Example finding", Severity: "warning",
		Path: "script.sh", Start: source.Position{Line: 2, Column: 3},
		LifecycleEvidence: &lifecycle.Evidence{Subject: lifecycle.LanguageRuntimes, Name: "example"},
		End:               source.Position{Line: 2, Column: 9},
		Source:            results.DiagnosticSource{Content: []byte("first\nsecond\n"), RawScript: true},
		MetadataIdentity:  &findingmodel.MetadataIdentity{Key: findingmodel.MetadataKey{Path: "action.yml", Message: "Example finding"}, Visit: 7},
	}
	adapter := NewChecksVisitor(rules, declaration.Name, Checks{
		Workflow: func(workflowdecl.Workflow) []reporting.StepFinding { return []reporting.StepFinding{{Finding: want}} },
	})
	if err := adapter.VisitWorkflowPre(workflowdecl.Workflow{}); err != nil {
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
