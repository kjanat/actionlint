package declarations

import (
	"reflect"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestDeclarationAdapterPreservesInternalRecord(t *testing.T) {
	t.Parallel()
	declaration := rule.Declaration{Name: "native-record", Category: "correctness"}
	rules, err := repoconfig.NewRuleSet([]rule.Declaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	want := findingmodel.Finding{
		Rule: "native-record", Code: "example", Message: "Example finding", Severity: "warning",
		Path: "script.sh", Start: source.Position{Line: 2, Column: 3},
		LifecycleEvidence: &lifecycle.Evidence{Subject: lifecycle.LanguageRuntimes, Name: "example"},
		EndColumn:         8,
		EndPosition:       &workflowlang.Pos{Line: 2, Col: 9},
		Source:            []byte("first\nsecond\n"),
		RawScriptSource:   true,
		MetadataIdentity:  &findingmodel.MetadataIdentity{Key: findingmodel.MetadataKey{Path: "action.yml", Message: "Example finding"}, Visit: 7},
	}
	adapter := New(rules, checkinputs.DeclarationCheck{
		Declaration: declaration,
		Workflow:    func(workflowdecl.WorkflowView) []reporting.Finding { return []reporting.Finding{{Finding: want}} },
	})
	if err := adapter.VisitWorkflowPre(workflowdecl.WorkflowView{}); err != nil {
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
