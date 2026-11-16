package correctness_test

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/executor"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func TestDeprecatedCommandsCheckerUsesItsRuleDeclaration(t *testing.T) {
	t.Parallel()
	declaration := correctness.DeprecatedCommandsRule.ResolvedDeclaration()
	checker := correctness.DeprecatedCommandsRule.Registration(rule.Declaration{}).Script
	descriptor := checker.Descriptor()
	if !reflect.DeepEqual(descriptor.Descriptor, declaration.Descriptor) ||
		descriptor.Scope != declaration.Scope || descriptor.SupportsActions != declaration.SupportsActions {
		t.Fatalf("checker metadata differs from its rule declaration: %+v", descriptor)
	}
	descriptor.RequiredInputs[0] = rule.Source
	if checker.Descriptor().RequiredInputs[0] != rule.Scripts {
		t.Fatal("caller changed the checker's input requirements")
	}
	registration := checkinputs.Registration{Checker: checker}
	if _, err := executor.Prepare(registration, []rule.InputKind{rule.Scripts}); err == nil {
		t.Fatal("prepared deprecated-command checking without invoked-script facts")
	}
	if _, err := executor.Prepare(registration, []rule.InputKind{rule.Scripts, rule.Invocations}); err != nil {
		t.Fatal(err)
	}
}
