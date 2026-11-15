package composites

import (
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	"actionlint.kjanat.dev/v2/internal/input/source"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"

	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type sourceMutationRule struct {
	workflowvisit.Base
	mutate   func(workflowdecl.Step)
	visits   int
	metadata int
}

func (r *sourceMutationRule) VisitStep(step workflowdecl.Step) error {
	r.visits++
	r.mutate(step)
	return nil
}

func (r *sourceMutationRule) EnterInvocation(checkcalls.Invocation) error { return nil }
func (r *sourceMutationRule) LeaveInvocation(invocation.Context) error    { return nil }
func (r *sourceMutationRule) VisitInvocationStep(step workflowdecl.Step) error {
	return r.VisitStep(step)
}

func (r *sourceMutationRule) VisitOpaqueAction(step workflowdecl.Step) error {
	return r.VisitStep(step)
}

func (r *sourceMutationRule) ForMetadata(*workspaceactions.Resolver) checkcalls.MetadataParticipant {
	r.metadata++
	return r
}

func (r *sourceMutationRule) CheckStandaloneMetadata(*actionmeta.Metadata, parsing.SourceSnapshot, actionmeta.ReferencedFiles) error {
	return nil
}

var (
	_ checkcalls.MetadataParticipant   = (*sourceMutationRule)(nil)
	_ checkcalls.InvocationParticipant = (*sourceMutationRule)(nil)
)

type canonicalFactObserver struct {
	workflowvisit.Base
	source     parsing.SourceSnapshot
	parents    []parsing.SourceSnapshot
	uses       *[]string
	runs       *[]string
	conditions *[]string
	reference  *[]source.Reference
	valid      *bool
}

func newCanonicalFactObserver(snapshot parsing.SourceSnapshot) *canonicalFactObserver {
	uses := []string{}
	runs := []string{}
	conditions := []string{}
	references := []source.Reference{}
	valid := true
	return &canonicalFactObserver{Base: workflowvisit.NewBase("observe-canonical-source", ""), source: snapshot, uses: &uses, runs: &runs, conditions: &conditions, reference: &references, valid: &valid}
}

func (observer *canonicalFactObserver) VisitStep(view workflowdecl.Step) error {
	if err := observer.source.ValidateStep(view); err != nil {
		*observer.valid = false
		return err
	}
	reference := view.Reference()
	*observer.reference = append(*observer.reference, reference)
	condition := view.Condition()
	if condition.State() != value.Missing {
		semantic := condition.Source()
		if boolean, known := condition.Bool(); known {
			semantic = strconv.FormatBool(boolean)
		}
		*observer.conditions = append(*observer.conditions, strconv.Itoa(int(condition.State()))+"\x00"+semantic)
	}
	if action, ok := view.Action(); ok {
		*observer.uses = append(*observer.uses, action.Uses.Text())
	}
	if run, ok := view.Run(); ok {
		*observer.runs = append(*observer.runs, run.Run.Text())
	}
	return nil
}

func (observer *canonicalFactObserver) EnterInvocation(call checkcalls.Invocation) error {
	observer.parents = append(observer.parents, observer.source)
	observer.source = call.Source
	return nil
}

func (observer *canonicalFactObserver) LeaveInvocation(invocation.Context) error {
	observer.source = observer.parents[len(observer.parents)-1]
	observer.parents = observer.parents[:len(observer.parents)-1]
	return nil
}

func (observer *canonicalFactObserver) VisitInvocationStep(step workflowdecl.Step) error {
	return observer.VisitStep(step)
}

func (observer *canonicalFactObserver) VisitOpaqueAction(step workflowdecl.Step) error {
	return observer.VisitStep(step)
}

var _ checkcalls.InvocationParticipant = (*canonicalFactObserver)(nil)

func TestSourceOwnershipRuleCannotChangeCollectedDefaultShell(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"workflow", "job"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			workflow := "on: push\ndefaults: {run: {shell: bash}}\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo original}]}}\n"
			if level == "job" {
				workflow = "on: push\njobs: {test: {runs-on: ubuntu-latest, defaults: {run: {shell: bash}}, steps: [{run: echo original}]}}\n"
			}
			f := newInvocationFixture(t, t.Context(), workflow, nil, nil)
			parsed := f.original
			defaults := parsed.Defaults
			if level == "job" {
				defaults = parsed.Jobs["test"].Defaults
			}
			probe := &sourceMutationRule{Base: workflowvisit.NewBase("mutate-default", ""), mutate: func(workflowdecl.Step) {
				defaults.Run.Shell.Value = "pwsh"
			}}
			f.options.Rules = append(f.options.Rules, probe)
			result, err := Workflow(f.options, f.root)
			if err != nil {
				t.Fatal(err)
			}
			units := result.Batch.Scripts()
			if probe.visits != 1 || len(units) != 1 || units[0].Shell == nil {
				t.Fatalf("default mutation scenario not exercised: visits=%d scripts=%d", probe.visits, len(units))
			}
			if got := *units[0].Shell; got != "bash" {
				t.Fatalf("rule mutation changed captured %s default shell: %q", level, got)
			}
		})
	}
}

func TestNestedCallResolutionUsesCanonicalDeclarationAfterMetadataCallback(t *testing.T) {
	t.Parallel()
	workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n"
	actions := map[string]string{
		"outer/action.yml": "name: outer\ndescription: test\nruns: {using: composite, steps: [{uses: $/a}]}\n",
		"a/action.yml":     "name: a\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo original-a}]}\n",
		"b/action.yml":     "name: b\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo mutated-b}]}\n",
	}
	f := newInvocationFixture(t, t.Context(), workflow, actions, nil)
	captureMutableMetadata(f)
	mutations := 0
	probe := &sourceMutationRule{Base: workflowvisit.NewBase("mutate-nested-call", ""), mutate: func(step workflowdecl.Step) {
		if action, ok := step.Action(); ok && action.Uses.Text() == "$/a" {
			mutateCallInput(t, f, step, "$/b")
			mutations++
		}
	}}
	f.options.Rules = append([]workflowvisit.Rule{probe}, f.options.Rules...)
	result, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if probe.metadata != 1 || mutations != 1 {
		t.Fatalf("nested metadata callback did not exercise mutation: callbacks=%d mutations=%d", probe.metadata, mutations)
	}
	var original bool
	for _, call := range result.Batch.Invocations().Calls() {
		facts, ok := call.Facts()
		if !ok || facts.Uses != "$/a" {
			continue
		}
		target, resolved := call.Outcome.Target()
		if !resolved || filepath.Base(filepath.Dir(target.Document)) != "a" {
			t.Fatalf("nested canonical uses and target diverged: facts=%+v target=%+v resolved=%v", facts, target, resolved)
		}
		original = true
	}
	if !original || len(result.Batch.Scripts()) != 1 || result.Batch.Scripts()[0].Run.Run.Text() != "echo original-a" {
		t.Fatalf("nested callback redirected canonical call: original=%v scripts=%+v", original, result.Batch.Scripts())
	}
}

type workflowPreCallMutation struct {
	workflowvisit.Base
	original  *workflowlang.Workflow
	mutations int
}

type workflowPreMutation struct {
	workflowvisit.Base
	mutate   func(*workflowlang.Workflow)
	original *workflowlang.Workflow
	runs     int
}

func (rule *workflowPreMutation) VisitWorkflowPre(_ workflowdecl.Workflow) error {
	rule.runs++
	rule.mutate(rule.original)
	return nil
}

func (rule *workflowPreCallMutation) VisitWorkflowPre(_ workflowdecl.Workflow) error {
	step := rule.original.Jobs["test"].Steps[0]
	action := step.Exec.(*workflowlang.ExecAction)
	action.Uses.Value = "$/b"
	step.Pos.Line = 99
	step.Pos.Col = 88
	rule.mutations++
	return nil
}

func TestCheckoutFactsStayBoundToCanonicalStepBeforeTraversal(t *testing.T) {
	t.Parallel()
	content := "on: push\njobs:\n  test:\n    runs-on: windows-latest\n    steps:\n      - uses: actions/checkout@v4\n        with: {path: repo}\n      - run: echo original\n"
	f := newInvocationFixture(t, t.Context(), content, nil, nil)
	baseline, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	baselineScopes := checkoutSnapshot(baseline)
	workflow := f.original
	probe := &workflowPreMutation{original: f.original, Base: workflowvisit.NewBase("mutate-checkout", ""), mutate: func(*workflowlang.Workflow) {
		checkout := workflow.Jobs["test"].Steps[0].Exec.(*workflowlang.ExecAction)
		checkout.Uses.Value = "actions/checkout@v5"
		checkout.Inputs["path"].Value.Value = "mutated-repo"
	}}
	f.options.Rules = append([]workflowvisit.Rule{probe}, f.options.Rules...)
	mutated, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if probe.runs != 1 {
		t.Fatalf("checkout mutation did not run: %d", probe.runs)
	}
	if got := checkoutSnapshot(mutated); !reflect.DeepEqual(got, baselineScopes) {
		t.Fatalf("legacy checkout mutations changed canonical placement:\n got: %#v\nwant: %#v", got, baselineScopes)
	}
}

type canonicalObservationSnapshot struct {
	Uses, Runs, Conditions []string
	References             []source.Reference
}

func canonicalObservation(observer *canonicalFactObserver) canonicalObservationSnapshot {
	return canonicalObservationSnapshot{
		Uses:       append([]string(nil), (*observer.uses)...),
		Runs:       append([]string(nil), (*observer.runs)...),
		Conditions: append([]string(nil), (*observer.conditions)...),
		References: append([]source.Reference(nil), (*observer.reference)...),
	}
}

type executionFactSnapshot struct {
	Calls       []string
	Targets     []string
	ScriptTexts []string
	Shells      []string
	Directories []string
	JobIDs      []string
}

func executionSnapshot(result Result) executionFactSnapshot {
	var snapshot executionFactSnapshot
	for _, call := range result.Batch.Invocations().Calls() {
		if facts, ok := call.Facts(); ok {
			snapshot.Calls = append(snapshot.Calls, facts.Uses)
		}
		if target, ok := call.Outcome.Target(); ok {
			snapshot.Targets = append(snapshot.Targets, target.Document)
		}
	}
	for index, unit := range result.Batch.Scripts() {
		snapshot.ScriptTexts = append(snapshot.ScriptTexts, unit.Run.Run.Text())
		snapshot.Shells = append(snapshot.Shells, unit.Execution.Shell.Name)
		snapshot.Directories = append(snapshot.Directories, unit.Execution.Directory.Path)
		fact, ok := result.Batch.Fact(index, true)
		if ok && fact.Runtime != nil {
			snapshot.Shells[index] = fact.Runtime.Shell
			snapshot.Directories[index] = fact.Runtime.Directory
		}
		if unit.Origin != nil {
			snapshot.JobIDs = append(snapshot.JobIDs, unit.Origin.Job)
		}
	}
	return snapshot
}

func checkoutSnapshot(result Result) []string {
	var paths []string
	for _, scope := range result.Batch.Scopes() {
		if placement := scope.Paths.Placements; placement.Present() {
			paths = append(paths, placement.Details().Directory.Path)
		}
	}
	return paths
}

func captureMutableMetadata(f *invocationFixture) {
	f.mutableMetadata = map[string]*actionmeta.Metadata{}
	f.mutateMeta = func(meta *actionmeta.Metadata) { f.mutableMetadata[filepath.Clean(meta.Path())] = meta }
}

func mutateRunInput(t *testing.T, f *invocationFixture, view workflowdecl.Step, text string) {
	t.Helper()
	if view.SourceReference() == f.root.Reference() {
		for key, job := range f.original.Jobs {
			owned := workflowStepViews(t, workflowView(t, f.root), key)
			for index, step := range job.Steps {
				if owned[index].Reference() == view.Reference() {
					step.Exec.(*workflowlang.ExecRun).Run.Value = text
					return
				}
			}
		}
	} else if meta := f.mutableMetadata[filepath.Clean(view.SourceReference().Document)]; meta != nil {
		for _, step := range meta.Runs.Steps {
			if step.Run != nil {
				step.Run.Value = text
				mutateMetadataScalar(step, "run", text)
				return
			}
		}
	}
	t.Fatal("mutable run input unavailable")
}

func mutateCallInput(t *testing.T, f *invocationFixture, view workflowdecl.Step, text string) {
	t.Helper()
	if view.SourceReference() == f.root.Reference() {
		for key, job := range f.original.Jobs {
			owned := workflowStepViews(t, workflowView(t, f.root), key)
			for index, step := range job.Steps {
				if owned[index].Reference() == view.Reference() {
					step.Exec.(*workflowlang.ExecAction).Uses.Value = text
					return
				}
			}
		}
	} else if meta := f.mutableMetadata[filepath.Clean(view.SourceReference().Document)]; meta != nil {
		for _, step := range meta.Runs.Steps {
			if step.Uses != nil && *step.Uses == "$/a" {
				*step.Uses = text
				mutateMetadataScalar(step, "uses", text)
				return
			}
		}
	}
	t.Fatal("mutable call input unavailable")
}

func mutateMetadataScalar(step *actionmeta.Step, key, value string) {
	for i := 0; i+1 < len(step.Node.Content); i += 2 {
		if step.Node.Content[i].Value == key {
			step.Node.Content[i+1].Value = value
		}
	}
}
