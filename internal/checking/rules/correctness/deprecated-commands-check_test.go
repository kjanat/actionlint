package correctness_test

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/executor"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
)

func TestDeprecatedCommandsCheckerUsesItsRuleDeclaration(t *testing.T) {
	t.Parallel()
	declaration := correctness.DeprecatedCommandsDeclaration()
	descriptor := (correctness.DeprecatedCommandsChecker{}).Descriptor()
	if !reflect.DeepEqual(descriptor.Descriptor, declaration.Descriptor) ||
		descriptor.Scope != declaration.Scope || descriptor.SupportsActions != declaration.SupportsActions {
		t.Fatalf("checker metadata differs from its rule declaration: %+v", descriptor)
	}
	descriptor.RequiredInputs[0] = rule.Source
	if (correctness.DeprecatedCommandsChecker{}).Descriptor().RequiredInputs[0] != rule.Scripts {
		t.Fatal("caller changed the checker's input requirements")
	}
	registration := checkinputs.Registration{Checker: correctness.DeprecatedCommandsChecker{}}
	if _, err := executor.Prepare(registration, []rule.InputKind{rule.Scripts}); err == nil {
		t.Fatal("prepared deprecated-command checking without invoked-script facts")
	}
	if _, err := executor.Prepare(registration, []rule.InputKind{rule.Scripts, rule.Invocations}); err != nil {
		t.Fatal(err)
	}
}
