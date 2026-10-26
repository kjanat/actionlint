package local

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/fsread"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/source"
)

func TestCompositeActionPathCasing(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "local/Nested/lib.sh", "VALUE=42\n")
	wantDirectory, err := filepath.EvalSymlinks(filepath.Join(root, "local", "Nested"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, checkout, spec, want string }{
		{"root", "", "./LOCAL/nested", "local/Nested"},
		{"placed", "Source", "./source/LOCAL/nested", "Source/local/Nested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := &Resolver{caseInsensitive: true}
			actions.restoreCheckout(&execution.CheckoutPlacement{Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: tc.checkout}, CaseInsensitive: true})
			paths := execution.Paths{Workspace: root, Analysis: root, Platform: runnerlang.Windows, Placements: actions.CheckoutState(), Reader: fsread.New(t.Context(), nil, nil)}
			call := &workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: tc.spec}}}
			facts, err := execution.CallFacts(call, paths.Platform)
			if err != nil {
				t.Fatal(err)
			}
			builder, err := invocation.NewBuilder(invocation.EntryWorkflow, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := builder.Begin(builder.RootContext(), source.Reference{Document: "fixture", Node: "call"}, facts)
			if err != nil {
				t.Fatal(err)
			}
			context, admitted := ticket.Context()
			if !admitted {
				t.Fatal("fixture call was not admitted")
			}
			execution.ActionOrigin(&paths, context, filepath.Join(root, "local", "Nested"))
			if paths.ActionRunnerPath != tc.want {
				t.Fatalf("runner action path=%q, want %q", paths.ActionRunnerPath, tc.want)
			}
			directory := paths.Resolve(paths.WorkingDirectory(&workflowlang.String{Value: "${{ github.action_path }}"}))
			if directory.Kind != execution.DirectoryKnown || directory.Path != wantDirectory {
				t.Fatalf("action source directory unresolved: %+v", directory)
			}
		})
	}
}
