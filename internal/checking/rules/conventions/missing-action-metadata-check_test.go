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
	remote := actionremote.New(metadataAvailabilityProvider{availability.Failed()})
	if _, err := remote.Inspect(t.Context(), "actions/checkout@v4"); err != nil {
		t.Fatal(err)
	}
	check := MissingActionMetadataCheck(remote.Resolved())
	for _, uses := range []string{"./local", "$/mapped", "docker://alpine", "${{ inputs.action }}", "invalid", "actions/checkout@v4"} {
		t.Run(uses, func(t *testing.T) {
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
	const source = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{uses: actions/checkout@v4}]\n"
	for _, tc := range []struct {
		reason availability.Unavailability
		want   int
	}{
		{availability.LookupDisabled(), 0},
		{availability.Offline(), 0},
		{availability.CacheMiss(), 0},
		{availability.ResolverNotConfigured(), 0},
		{availability.Failed(), 1},
		{availability.Inaccessible(), 1},
		{availability.Unsupported(), 1},
		{availability.UnsupportedHost(), 1},
		{availability.UnsupportedObjectFormat(), 1},
		{availability.InvalidResolverResult(), 1},
	} {
		t.Run(string(tc.reason.Reason()), func(t *testing.T) {
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
				switch tc.reason.Reason() {
				case availability.UnsupportedHostReason:
					if !strings.Contains(got[0].Message, "only supports github.com") {
						t.Fatal(got[0].Message)
					}
				case availability.UnsupportedObjectFormatReason:
					if !strings.Contains(got[0].Message, "SHA-256") || !strings.Contains(got[0].Message, "SHA-1") {
						t.Fatal(got[0].Message)
					}
				case availability.InvalidResolverResultReason:
					if !strings.Contains(got[0].Message, "--resolve-with") {
						t.Fatal(got[0].Message)
					}
				}
			}
		})
	}
}
