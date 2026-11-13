package conventions

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/availability"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestMissingActionMetadataUsesIndependentStepInputs(t *testing.T) {
	t.Parallel()
	remote := actionremote.New(metadataAvailabilityProvider{availability.Failed()})
	if _, err := remote.Inspect(t.Context(), "actions/checkout@v4"); err != nil {
		t.Fatal(err)
	}
	check := MissingActionMetadataCheck(remote.Resolved())
	for _, uses := range []string{"./local", "$/mapped", "docker://alpine", "${{ inputs.action }}", "invalid", "actions/checkout@v4"} {
		t.Run(uses, func(t *testing.T) {
			t.Parallel()
			step := &workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: uses, Pos: &workflowlang.Pos{Line: 7, Col: 9}}}}
			snapshot := workflowdecl.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Steps: []*workflowlang.Step{step}}}}, false, "").Bind("metadata-step").Workflow()
			job, _ := snapshot.Job("test")
			var checkedStep workflowdecl.StepView
			for step := range job.Steps() {
				checkedStep = step
				break
			}
			for range 2 {
				findings := check.Step(checkedStep)
				if uses != "actions/checkout@v4" {
					if len(findings) != 0 {
						t.Fatalf("non-repository reference was checked: %+v", findings)
					}
					continue
				}
				if len(findings) != 1 || !strings.Contains(findings[0].Message, "failed") || findings[0].Start.Line != 7 || findings[0].Start.Column != 9 || findings[0].Step.Reference() != checkedStep.Reference() {
					t.Fatalf("missing metadata finding lost its outcome or source: %+v", findings)
				}
			}
		})
	}
}

type metadataAvailabilityProvider struct{ reason availability.Unavailability }

func (p metadataAvailabilityProvider) Lookup(context.Context, actions.Reference) (actions.Manifest, error) {
	return actions.UnavailableManifest(p.reason), nil
}

func TestMissingMetadataDoesNotReportDeliberatelySkippedLookups(t *testing.T) {
	t.Parallel()
	const source = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{uses: actions/checkout@v4}]\n"
	for _, tc := range []struct {
		reason   availability.Unavailability
		want     int
		wantHelp string
	}{
		{availability.LookupDisabled(), 0, ""},
		{availability.Offline(), 0, ""},
		{availability.CacheMiss(), 0, ""},
		{availability.ResolverNotConfigured(), 0, ""},
		{availability.NotFetched(), 1, ""},
		{availability.Denied(), 1, ""},
		{availability.Failed(), 1, ""},
		{availability.NotFound(), 1, ""},
		{availability.Inaccessible(), 1, ""},
		{availability.Limit(), 1, ""},
		{availability.Unsupported(), 1, ""},
		{availability.UnsupportedHost(), 1, "; the GitHub metadata provider only supports github.com"},
		{availability.UnsupportedObjectFormat(), 1, "; metadata lookup supports SHA-1 commit IDs; use a branch or tag name instead"},
		{availability.InvalidResolverResult(), 1, "; the reference resolver returned an invalid or incomplete selection"},
	} {
		t.Run(string(tc.reason.Reason()), func(t *testing.T) {
			t.Parallel()
			remote := actionremote.New(metadataAvailabilityProvider{tc.reason})
			if _, err := remote.Inspect(t.Context(), "actions/checkout@v4"); err != nil {
				t.Fatal(err)
			}
			step := &workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "actions/checkout@v4", Pos: &workflowlang.Pos{Line: 5, Col: 20}}}}
			job, _ := workflowdecl.Publish(source, &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Steps: []*workflowlang.Step{step}}}}, false, "").Bind("metadata-availability").Workflow().Job("test")
			for view := range job.Steps() {
				got := MissingActionMetadataCheck(remote.Resolved()).Step(view)
				if len(got) != tc.want {
					t.Fatalf("findings=%+v, want %d", got, tc.want)
				}
				if tc.want == 1 && !strings.Contains(got[0].Message, string(tc.reason.Reason())) {
					t.Fatalf("diagnostic omits reason: %+v", got[0])
				}
				if tc.want == 1 {
					want := "could not inspect metadata for actions/checkout@v4 (" + string(tc.reason.Reason()) + ")" + tc.wantHelp
					if got[0].Message != want {
						t.Fatalf("message=%q, want %q", got[0].Message, want)
					}
				}
			}
		})
	}
}
