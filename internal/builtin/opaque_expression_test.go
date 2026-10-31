package builtin

import (
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func TestOpaqueActionExpressionsUseInvocationFrameWithoutWorkflowDuplicates(t *testing.T) {
	for _, inside := range []bool{false, true} {
		t.Run(map[bool]string{false: "workflow already checked", true: "invoked opaque input"}[inside], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "action.yml")
			content := []byte("name: opaque\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/github-script@v7\n      with:\n        script: ${{ github.event.issue.title }}\n")
			registry := declarations.NewCapturedSnapshots(t, root)
			snapshot, err := registry.Document(path, content, documentparse.Action(path, path, content))
			if err != nil {
				t.Fatal(err)
			}
			body, present := snapshot.ActionView()
			steps := slices.Collect(body.Steps())
			if !present || len(steps) != 1 {
				t.Fatal("captured opaque action step absent")
			}
			rule := newRuleExpression(nil, nil, nil)
			if inside {
				builder, err := invocation.NewBuilder(invocation.EntryAction, snapshot.Reference(), invocation.DefaultBudget())
				if err != nil {
					t.Fatal(err)
				}
				context := builder.RootContext()
				if err := rule.EnterInvocation(Invocation{Context: context, Source: snapshot, Path: path}); err != nil {
					t.Fatal(err)
				}
				if err := rule.VisitOpaqueAction(steps[0]); err != nil {
					t.Fatal(err)
				}
				if err := rule.LeaveInvocation(context); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := rule.VisitStep(steps[0]); err != nil {
					t.Fatal(err)
				}
				if err := rule.VisitOpaqueAction(steps[0]); err != nil {
					t.Fatal(err)
				}
			}
			findings := rule.Errs()
			if len(findings) != 1 || findings[0].Rule != "script-injection" || len(rule.frames) != 0 {
				t.Fatalf("opaque input omitted, duplicated or leaked a frame: %+v", findings)
			}
		})
	}
}
