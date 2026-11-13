package correctness

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func TestActionEntryInitializesPathsAndPreservesCheckerFlags(t *testing.T) {
	t.Parallel()
	parent := NewExecutableChecker(ExecutableContext{})
	builder, err := invocation.NewBuilder(invocation.EntryAction, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	parent.jobEnv, parent.jobGitEnv, parent.jobPathUnknown, parent.skipFindings = true, true, true, true
	child := parent.ForkComposite(builder.RootContext(), "action-source", execution.Paths{ActionRunnerPath: "stale", ActionIndependent: true})
	if child.paths.ActionPath != "action-source" || child.paths.ActionRunnerPath != "" || child.paths.ActionIndependent {
		t.Fatalf("entry inherited caller origin: %+v", child.paths)
	}
	if !child.jobEnv || !child.jobGitEnv || !child.jobPathUnknown || !child.skipFindings {
		t.Fatal("entry lost prepared checker flags")
	}
}
