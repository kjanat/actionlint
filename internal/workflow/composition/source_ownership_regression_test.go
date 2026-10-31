package composition

import (
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/source"
)

type sourceMutationRule struct {
	workflowvisit.Base
	mutate   func(workflowdecl.StepView)
	visits   int
	metadata int
}

func (r *sourceMutationRule) VisitStep(step workflowdecl.StepView) error {
	r.visits++
	r.mutate(step)
	return nil
}

func (r *sourceMutationRule) EnterInvocation(builtin.Invocation) error { return nil }
func (r *sourceMutationRule) LeaveInvocation(invocation.Context) error { return nil }
func (r *sourceMutationRule) VisitInvocationStep(step workflowdecl.StepView) error {
	return r.VisitStep(step)
}

func (r *sourceMutationRule) VisitOpaqueAction(step workflowdecl.StepView) error {
	return r.VisitStep(step)
}

func (r *sourceMutationRule) ForMetadata(*actionlocal.Resolver) workflowvisit.Rule {
	r.metadata++
	return r
}

func (r *sourceMutationRule) CheckStandaloneMetadata(*actionmeta.Metadata, documentparse.SourceSnapshot) error {
	return nil
}

var (
	_ builtin.MetadataParticipant   = (*sourceMutationRule)(nil)
	_ builtin.InvocationParticipant = (*sourceMutationRule)(nil)
)

type canonicalFactObserver struct {
	workflowvisit.Base
	source     documentparse.SourceSnapshot
	parents    []documentparse.SourceSnapshot
	uses       *[]string
	runs       *[]string
	conditions *[]string
	reference  *[]source.Reference
	valid      *bool
}

func newCanonicalFactObserver(snapshot documentparse.SourceSnapshot) *canonicalFactObserver {
	uses := []string{}
	runs := []string{}
	conditions := []string{}
	references := []source.Reference{}
	valid := true
	return &canonicalFactObserver{Base: workflowvisit.NewBase("observe-canonical-source", ""), source: snapshot, uses: &uses, runs: &runs, conditions: &conditions, reference: &references, valid: &valid}
}

func (observer *canonicalFactObserver) VisitStep(view workflowdecl.StepView) error {
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
		*observer.uses = append(*observer.uses, action.Uses().Value())
	}
	if run, ok := view.Run(); ok {
		*observer.runs = append(*observer.runs, run.Run().Value())
	}
	return nil
}

func (observer *canonicalFactObserver) EnterInvocation(call builtin.Invocation) error {
	observer.parents = append(observer.parents, observer.source)
	observer.source = call.Source
	return nil
}

func (observer *canonicalFactObserver) LeaveInvocation(invocation.Context) error {
	observer.source = observer.parents[len(observer.parents)-1]
	observer.parents = observer.parents[:len(observer.parents)-1]
	return nil
}

func (observer *canonicalFactObserver) VisitInvocationStep(step workflowdecl.StepView) error {
	return observer.VisitStep(step)
}

func (observer *canonicalFactObserver) VisitOpaqueAction(step workflowdecl.StepView) error {
	return observer.VisitStep(step)
}

var _ builtin.InvocationParticipant = (*canonicalFactObserver)(nil)

func TestSourceOwnershipRuleCannotChangeCollectedScript(t *testing.T) {
	for _, composite := range []bool{false, true} {
		name := "workflow"
		workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo original}]}}\n"
		var actions map[string]string
		if composite {
			name = "composite"
			workflow = "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/child}]}}\n"
			actions = map[string]string{"child/action.yml": "name: child\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo original}]}\n"}
		}
		t.Run(name, func(t *testing.T) {
			f := newInvocationFixture(t, t.Context(), workflow, actions, nil)
			captureMutableMetadata(f)
			mutations := 0
			probe := &sourceMutationRule{Base: workflowvisit.NewBase("mutate-source", ""), mutate: func(step workflowdecl.StepView) {
				if run, ok := step.Run(); ok && run.Run().Present() {
					mutateRunInput(t, f, step, "echo mutated after collection")
					mutations++
				}
			}}
			f.options.Rules = append(f.options.Rules, probe)
			result, err := Workflow(f.options, f.root)
			if err != nil {
				t.Fatal(err)
			}
			units := result.Batch.Scripts()
			if mutations != 1 || len(units) != 1 || !units[0].Run.Present() || !units[0].Run.Run().Present() {
				t.Fatalf("mutation scenario not exercised: mutations=%d scripts=%d", mutations, len(units))
			}
			if got := units[0].Run.Run().Value(); got != "echo original" {
				t.Fatalf("rule mutation after collection changed canonical script: %q", got)
			}
		})
	}
}

func TestSourceOwnershipRuleCannotChangeCollectedDefaultShell(t *testing.T) {
	for _, level := range []string{"workflow", "job"} {
		t.Run(level, func(t *testing.T) {
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
			probe := &sourceMutationRule{Base: workflowvisit.NewBase("mutate-default", ""), mutate: func(workflowdecl.StepView) {
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

func TestCallResolutionUsesCanonicalDeclarationAfterCallbackMutation(t *testing.T) {
	for _, composite := range []bool{false, true} {
		name := "workflow"
		workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/a}]}}\n"
		actions := map[string]string{
			"a/action.yml": "name: a\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo original-a}]}\n",
			"b/action.yml": "name: b\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo mutated-b}]}\n",
		}
		if composite {
			name = "composite"
			workflow = "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n"
			actions["outer/action.yml"] = "name: outer\ndescription: test\nruns: {using: composite, steps: [{uses: $/a}]}\n"
		}
		t.Run(name, func(t *testing.T) {
			f := newInvocationFixture(t, t.Context(), workflow, actions, nil)
			captureMutableMetadata(f)
			mutations := 0
			probe := &sourceMutationRule{Base: workflowvisit.NewBase("mutate-call", ""), mutate: func(step workflowdecl.StepView) {
				if action, ok := step.Action(); ok && action.Uses().Value() == "$/a" {
					mutateCallInput(t, f, step, "$/b")
					mutations++
				}
			}}
			f.options.Rules = append([]workflowvisit.Rule{probe}, f.options.Rules...)
			result, err := Workflow(f.options, f.root)
			if err != nil {
				t.Fatal(err)
			}
			var canonicalCall bool
			for _, call := range result.Batch.Invocations().Calls() {
				facts, ok := call.Facts()
				if ok && facts.Uses == "$/a" {
					target, resolved := call.Outcome.Target()
					if !resolved || filepath.Base(filepath.Dir(target.Document)) != "a" {
						t.Fatalf("canonical uses and target diverged: facts=%+v target=%+v resolved=%v", facts, target, resolved)
					}
					canonicalCall = true
				}
			}
			if mutations != 1 || !canonicalCall || len(result.Batch.Scripts()) != 1 || result.Batch.Scripts()[0].Run.Run().Value() != "echo original-a" {
				t.Fatalf("callback redirected canonical call: mutations=%d metadata=%d canonical=%v scripts=%+v", mutations, probe.metadata, canonicalCall, result.Batch.Scripts())
			}
		})
	}
}

func TestNestedCallResolutionUsesCanonicalDeclarationAfterMetadataCallback(t *testing.T) {
	workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n"
	actions := map[string]string{
		"outer/action.yml": "name: outer\ndescription: test\nruns: {using: composite, steps: [{uses: $/a}]}\n",
		"a/action.yml":     "name: a\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo original-a}]}\n",
		"b/action.yml":     "name: b\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo mutated-b}]}\n",
	}
	f := newInvocationFixture(t, t.Context(), workflow, actions, nil)
	captureMutableMetadata(f)
	mutations := 0
	probe := &sourceMutationRule{Base: workflowvisit.NewBase("mutate-nested-call", ""), mutate: func(step workflowdecl.StepView) {
		if action, ok := step.Action(); ok && action.Uses().Value() == "$/a" {
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
	if !original || len(result.Batch.Scripts()) != 1 || result.Batch.Scripts()[0].Run.Run().Value() != "echo original-a" {
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

func (rule *workflowPreMutation) VisitWorkflowPre(_ workflowdecl.WorkflowView) error {
	rule.runs++
	rule.mutate(rule.original)
	return nil
}

func (rule *workflowPreCallMutation) VisitWorkflowPre(_ workflowdecl.WorkflowView) error {
	step := rule.original.Jobs["test"].Steps[0]
	action := step.Exec.(*workflowlang.ExecAction)
	action.Uses.Value = "$/b"
	step.Pos.Line = 99
	step.Pos.Col = 88
	rule.mutations++
	return nil
}

func TestCallFactsStayBoundToCanonicalDeclarationBeforeTraversal(t *testing.T) {
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/a}]}}\n")
	actions := map[string]string{
		"a/action.yml": "name: a\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo original-a}]}\n",
		"b/action.yml": "name: b\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo mutated-b}]}\n",
	}
	f := newInvocationFixture(t, t.Context(), string(content), actions, nil)
	view := workflowStepViews(t, workflowView(t, f.root), "test")[0]
	wantFacts, err := workflowexecution.CallFacts(view, runnerlang.MacOrLinux)
	if err != nil {
		t.Fatal(err)
	}
	probe := &workflowPreCallMutation{original: f.original, Base: workflowvisit.NewBase("pre-mutate-call", "")}
	f.options.Rules = append(f.options.Rules, probe)
	result, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if probe.mutations != 1 {
		t.Fatalf("pre-traversal mutation did not run: %d", probe.mutations)
	}
	graph := result.Batch.Invocations()
	if len(graph.Calls()) != 1 {
		t.Fatalf("call graph diverged: %+v", graph.Calls())
	}
	facts, _ := graph.Calls()[0].Facts()
	if facts != wantFacts {
		t.Fatalf("graph facts came from mutable AST: got=%+v want=%+v", facts, wantFacts)
	}
	if len(result.Batch.Scripts()) != 1 || result.Batch.Scripts()[0].Run.Run().Value() != "echo original-a" {
		t.Fatalf("pre-traversal mutation redirected collected scripts: %+v", result.Batch.Scripts())
	}
	projected, ok := result.Batch.Fact(0, false)
	if !ok {
		t.Fatal("batch script projection missing")
	}
	call, exists := result.Batch.CallGraph().Call(projected.Occurrence.Owner)
	if !exists {
		t.Fatal("batch script owner call missing")
	}
	if call.Uses != wantFacts.Uses || call.Step.Line != wantFacts.Step.Line || call.Step.Column != wantFacts.Step.Column {
		t.Fatalf("batch public call facts differ from canonical graph: %+v", call)
	}
}

func TestExecutionFactsStayBoundToCanonicalWorkflowBeforeTraversal(t *testing.T) {
	content := "on: push\nenv: {ROOT_VALUE: original}\ndefaults: {run: {shell: bash, working-directory: source-dir}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    env: {JOB_VALUE: original}\n    container: alpine\n    steps:\n      - run: echo original\n"
	f := newInvocationFixture(t, t.Context(), content, nil, nil)
	baseline, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	baselineFacts := executionSnapshot(baseline)
	probe := &workflowPreMutation{original: f.original, Base: workflowvisit.NewBase("mutate-workflow", ""), mutate: func(workflow *workflowlang.Workflow) {
		if workflow.Env == nil || workflow.Defaults == nil || workflow.Defaults.Run == nil || workflow.Jobs["test"] == nil || workflow.Jobs["test"].Env == nil || workflow.Jobs["test"].Container == nil {
			t.Fatalf("workflow mutation fixture fields unavailable: env=%v defaults=%v job=%+v", workflow.Env != nil, workflow.Defaults != nil, workflow.Jobs["test"])
		}
		for _, variable := range workflow.Env.Vars {
			variable.Value.Value = "mutated"
		}
		workflow.Defaults.Run.Shell.Value = "pwsh"
		workflow.Defaults.Run.WorkingDirectory.Value = "mutated-dir"
		job := workflow.Jobs["test"]
		job.ID = &workflowlang.String{Value: "mutated-job"}
		for _, variable := range job.Env.Vars {
			variable.Value.Value = "mutated"
		}
		job.Container.Image.Value = "mutated-image"
		job.Steps[0].Exec.(*workflowlang.ExecRun).Run.Value = "echo mutated"
	}}
	f.options.Rules = append([]workflowvisit.Rule{probe}, f.options.Rules...)
	mutated, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if probe.runs != 1 {
		t.Fatalf("VisitWorkflowPre mutation did not run: %d", probe.runs)
	}
	if got := executionSnapshot(mutated); !reflect.DeepEqual(got, baselineFacts) {
		t.Fatalf("legacy workflow mutations changed canonical execution facts:\n got: %#v\nwant: %#v", got, baselineFacts)
	}
	if len(mutated.Batch.Scripts()) != 1 || mutated.Batch.Scripts()[0].Run.Run().Value() != "echo original" {
		t.Fatalf("mutated traversal changed canonical script: %+v", mutated.Batch.Scripts())
	}
}

func TestCheckoutFactsStayBoundToCanonicalStepBeforeTraversal(t *testing.T) {
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

func TestParallelChildrenStayBoundToCanonicalVariantsBeforeTraversal(t *testing.T) {
	content := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - parallel:\n          - if: ${{ true }}\n            run: echo original\n          - uses: $/a\n"
	actions := map[string]string{"a/action.yml": "name: a\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo action-a}]}\n", "b/action.yml": "name: b\ndescription: test\nruns: {using: composite, steps: [{shell: bash, run: echo action-b}]}\n"}
	f := newInvocationFixture(t, t.Context(), content, actions, nil)
	baseRules := f.options.Rules
	baselineObserver := newCanonicalFactObserver(f.root)
	f.options.Rules = append([]workflowvisit.Rule{baselineObserver}, baseRules...)
	baseline, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	baselineFacts := executionSnapshot(baseline)
	baselineObserved := canonicalObservation(baselineObserver)
	observer := newCanonicalFactObserver(f.root)
	workflow := f.original
	probe := &workflowPreMutation{original: f.original, Base: workflowvisit.NewBase("mutate-parallel", ""), mutate: func(*workflowlang.Workflow) {
		parallel := workflow.Jobs["test"].Steps[0].Exec.(*workflowlang.ExecParallel)
		run := parallel.Steps[0]
		run.Exec.(*workflowlang.ExecRun).Run.Value = "echo mutated"
		run.If.Value = "${{ false }}"
		action := parallel.Steps[1].Exec.(*workflowlang.ExecAction)
		action.Uses.Value = "$/b"
	}}
	f.options.Rules = append([]workflowvisit.Rule{probe, observer}, baseRules...)
	mutated, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if probe.runs != 1 {
		t.Fatalf("parallel mutation did not run: %d", probe.runs)
	}
	if got := executionSnapshot(mutated); !reflect.DeepEqual(got, baselineFacts) {
		t.Fatalf("parallel callback traversal changed canonical facts:\n got: %#v\nwant: %#v", got, baselineFacts)
	}
	calls := mutated.Batch.Invocations().Calls()
	if len(calls) != 1 {
		t.Fatalf("parallel action variant changed or registered twice: calls=%+v", calls)
	}
	facts, ok := calls[0].Facts()
	target, resolved := calls[0].Outcome.Target()
	if !ok || facts.Uses != "$/a" || !resolved || filepath.Base(filepath.Dir(target.Document)) != "a" {
		t.Fatalf("parallel graph lost canonical action variant: facts=%+v target=%+v resolved=%v", facts, target, resolved)
	}
	if len(mutated.Batch.Scripts()) != 2 || mutated.Batch.Scripts()[0].Run.Run().Value() != "echo original" || mutated.Batch.Scripts()[1].Run.Run().Value() != "echo action-a" {
		t.Fatalf("parallel canonical scripts changed: %+v", mutated.Batch.Scripts())
	}
	if !*observer.valid || len(*observer.reference) < 2 {
		t.Fatalf("canonical observer did not receive workflow and composite child scopes: valid=%t references=%+v", *observer.valid, *observer.reference)
	}
	if got := canonicalObservation(observer); !reflect.DeepEqual(got, baselineObserved) {
		t.Fatalf("plugin/shared-facts observer saw changed canonical source:\n got: %#v\nwant: %#v", got, baselineObserved)
	}
	if !slices.Contains(baselineObserved.Runs, "echo original") || !slices.Contains(baselineObserved.Runs, "echo action-a") || !slices.ContainsFunc(baselineObserved.Conditions, func(condition string) bool { return strings.Contains(condition, "true") }) {
		t.Fatalf("observer did not read original run and parallel condition values: %+v", baselineObserved)
	}
	seen := make(map[source.Reference]struct{}, len(*observer.reference))
	for _, reference := range *observer.reference {
		if _, duplicate := seen[reference]; duplicate {
			t.Fatalf("canonical observer visited a source reference twice: %+v", reference)
		}
		seen[reference] = struct{}{}
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
		snapshot.ScriptTexts = append(snapshot.ScriptTexts, unit.Run.Run().Value())
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

func mutateRunInput(t *testing.T, f *invocationFixture, view workflowdecl.StepView, text string) {
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

func mutateCallInput(t *testing.T, f *invocationFixture, view workflowdecl.StepView, text string) {
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
