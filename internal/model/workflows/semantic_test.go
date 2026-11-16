package workflows

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestPublishedSemanticsDoNotFollowLegacyMutation(t *testing.T) {
	t.Parallel()
	name := &definition.String{Value: "before", Pos: &source.Position{Line: 2, Column: 4}}
	condition := &definition.String{Value: "${{ true }}", Pos: &source.Position{Line: 3, Column: 7}}
	job := &definition.Job{
		Name:   name,
		If:     condition,
		RunsOn: &definition.Runner{Labels: []*definition.String{{Value: "ubuntu-latest"}}},
		Strategy: &definition.Strategy{Matrix: &definition.Matrix{Rows: map[string]*definition.MatrixRow{
			"os": {Name: &definition.String{Value: "os"}, Values: []definition.RawYAMLValue{
				&definition.RawYAMLString{Value: "ubuntu-latest", Tag: definition.YamlTagStr},
			}},
		}}},
		Env: &definition.Env{
			Vars: map[string]*definition.EnvVar{
				"defined":  {Name: &definition.String{Value: "defined"}, Value: &definition.String{Value: "present"}},
				"nullable": {Name: &definition.String{Value: "nullable"}, Value: &definition.String{Value: "${{ fromJSON('null') }}"}},
				"empty":    {Name: &definition.String{Value: "empty"}, Value: &definition.String{Value: ""}},
			},
		},
		Steps: []*definition.Step{{If: condition, Exec: &definition.ExecAction{Inputs: map[string]*definition.ActionArgument{
			"greeting": {Name: &definition.String{Value: "greeting"}, Value: &definition.String{Value: "hello"}},
		}}}},
	}
	workflow := &definition.Workflow{Jobs: map[string]*definition.Job{"build": job}}
	bound := Bind("semantic-test", "", workflow, false, "")
	workflowView := bound.Workflow()
	jobView, ok := workflowView.Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	if got, ok := Scalar(jobView.Name).Text(); !ok || got != "before" {
		t.Fatalf("published name = %q, %v; want before, true", got, ok)
	}
	var step Step
	for candidate := range jobView.Steps() {
		step = candidate
	}
	if got, ok := step.Condition().Bool(); !ok || !got {
		t.Fatalf("condition = %v, %v; want true", got, ok)
	}
	if !step.Condition().Origin().FromExpression {
		t.Fatal("condition lost expression origin")
	}
	if got, ok := Environment(jobView.Env).Lookup("defined").Text(); !ok || got != "present" {
		t.Fatalf("published environment value = %q, %v; want present, true", got, ok)
	}
	if state := Environment(jobView.Env).Lookup("unknown").State(); state != value.Missing {
		t.Fatalf("absent key in complete environment = %v; want Missing", state)
	}
	if got := Environment(jobView.Env).Lookup("nullable"); got.State() != value.Evaluated || got.Kind() != value.Null {
		t.Fatalf("explicit null environment value = (%v,%v); want Evaluated Null", got.State(), got.Kind())
	}
	if got, ok := Environment(jobView.Env).Lookup("empty").Text(); !ok || got != "" {
		t.Fatalf("explicit empty environment value = %q, %v; want empty string", got, ok)
	}
	actionView, ok := step.Action()
	if !ok {
		t.Fatal("action view missing")
	}
	if got, ok := ResolvedInputs(actionView).Lookup("greeting").Text(); !ok || got != "hello" {
		t.Fatalf("published action input = %q, %v; want hello, true", got, ok)
	}
	if got, ok := RunnerSelection(jobView.RunsOn).Prop("labels"); !ok || got.Len() != 1 {
		t.Fatalf("runner labels = %v, %v; want one label", got, ok)
	}
	if _, ok := RunnerSelection(jobView.RunsOn).Prop("expression"); ok {
		t.Fatal("literal runner selection contains synthetic expression property")
	}
	combinations, complete := MatrixResolution(jobView.Strategy).Combinations(10)
	if !complete || len(combinations) != 1 {
		t.Fatalf("matrix combinations = %d, %v; want one complete combination", len(combinations), complete)
	}
	if got, ok := combinations[0].Get("os"); !ok || gotText(got) != "ubuntu-latest" {
		t.Fatalf("matrix os = %v, %v; want ubuntu-latest", got, ok)
	}
}

func gotText(v value.Value) string {
	text, _ := v.ScalarString()
	return text
}

func TestPublishedConditionsIncludeNestedParallelStepsAndSnapshot(t *testing.T) {
	t.Parallel()
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    if: false
    snapshot:
      image-name: custom
      version: v1
      if: false
    steps:
      - parallel:
          - if: false
            run: echo static
          - if: ${{ github.ref == 'refs/heads/main' }}
            run: echo runtime
`
	workflow, errors := workflowparser.ParseWorkflow([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Bind("condition-test", source, workflow, false, "")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	if got, ok := Condition(job.If).Bool(); !ok || got {
		t.Fatalf("job condition = %v, %v; want false", got, ok)
	}
	if got, ok := Condition(job.Snapshot.If).Bool(); !ok || got {
		t.Fatalf("snapshot condition = %v, %v; want false", got, ok)
	}
	var parallel Step
	for step := range job.Steps() {
		parallel = step
	}
	group, ok := parallel.ParallelSteps()
	if !ok {
		t.Fatal("parallel step missing")
	}
	var children []Step
	for child := range group {
		children = append(children, child)
	}
	if len(children) != 2 {
		t.Fatalf("parallel children = %d; want 2", len(children))
	}
	if got, ok := children[0].Condition().Bool(); !ok || got {
		t.Fatalf("nested literal condition = %v, %v; want false", got, ok)
	}
	if state := children[1].Condition().State(); state != value.Dynamic {
		t.Fatalf("nested runtime condition state = %v; want Dynamic", state)
	}
}

func TestPublishedEnvironmentNullStatesFromParsedYAML(t *testing.T) {
	t.Parallel()
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      literal-null: null
      evaluated-null: ${{ fromJSON('null') }}
      blank:
      empty: ""
    steps:
      - run: echo ok
      - uses: actions/checkout@v4
        with:
          literal-null: null
          blank:
          empty: ""
`
	workflow, errors := workflowparser.ParseWorkflow([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Bind("null-test", source, workflow, false, "")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	// Legacy String text stays unchanged while the separate source snapshot keeps the YAML tag.
	if sourceValue := workflow.Jobs["build"].Env.Vars["literal-null"].Value.Value; sourceValue != "null" {
		t.Fatalf("parser literal-null value = %q; want observed text null", sourceValue)
	}
	if got := Environment(job.Env).Lookup("literal-null"); got.State() != value.Literal || got.Kind() != value.Null {
		t.Fatalf("parsed literal null = (%v,%v); want Literal Null", got.State(), got.Kind())
	}
	if got := Environment(job.Env).Lookup("evaluated-null"); got.State() != value.Evaluated || got.Kind() != value.Null {
		t.Fatalf("evaluated null = (%v,%v); want Evaluated Null", got.State(), got.Kind())
	}
	if got := Environment(job.Env).Lookup("empty"); got.State() != value.Literal || got.Kind() != value.String || gotText(got) != "" {
		t.Fatalf("empty string = (%v,%v,%q); want Literal empty String", got.State(), got.Kind(), gotText(got))
	}
	if got := Environment(job.Env).Lookup("blank"); got.State() != value.Literal || got.Kind() != value.Null {
		t.Fatalf("blank null = (%v,%v); want Literal Null", got.State(), got.Kind())
	}
	var actionView *definition.ExecAction
	for step := range job.Steps() {
		if action, ok := step.Action(); ok {
			actionView = action
		}
	}
	if actionView == nil {
		t.Fatal("action view missing")
	}
	for name, wantKind := range map[string]value.ValueKind{"literal-null": value.Null, "blank": value.Null, "empty": value.String} {
		got := ResolvedInputs(actionView).Lookup(name)
		if got.State() != value.Literal || got.Kind() != wantKind {
			t.Errorf("action input %q = (%v,%v); want Literal %v", name, got.State(), got.Kind(), wantKind)
		}
	}
	if got := ResolvedInputs(actionView).Lookup("missing"); got.State() != value.Missing {
		t.Errorf("omitted action input = %v; want Missing", got.State())
	}
}

func TestPublishedSourceScalarIsPointerFreeSnapshot(t *testing.T) {
	t.Parallel()
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      literal-null: null
    steps:
      - run: echo ok
`
	workflow, errors := workflowparser.ParseWorkflow([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Bind("source-scalar", source, workflow, false, "")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	view, ok := job.Env.Vars["literal-null"]
	if !ok || view == nil {
		t.Fatal("environment variable view missing")
	}
	got := view.Value.SourceScalar
	if !got.HasSource || got.Kind != codec.ScalarNode || got.Tag != definition.YamlTagNull || got.Value != "null" {
		t.Fatalf("published source scalar = %#v; want original YAML null", got)
	}
	manual := Bind("manual-source", "", &definition.Workflow{Jobs: map[string]*definition.Job{
		"manual": {Name: &definition.String{Value: "manual"}},
	}}, false, "")
	manualJob, ok := manual.Workflow().Job("manual")
	if !ok {
		t.Fatal("manual job view missing")
	}
	if got := manualJob.Name.SourceScalar; got.HasSource || got.Kind != 0 {
		t.Fatalf("manual string has source provenance: %#v", got)
	}
}

func TestStaticStringFoldPublishesEvaluatedDataAndSource(t *testing.T) {
	t.Parallel()
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: ${{ '${{ runtime }}' }}
        run: echo ok
`
	workflow, errors := workflowparser.ParseWorkflow([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Bind("static-fold", source, workflow, false, "")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	var step Step
	for candidate := range job.Steps() {
		step = candidate
	}
	name := step.ID
	if got, ok := Scalar(name).Text(); !ok || got != "${{ runtime }}" || Scalar(name).State() != value.Evaluated {
		t.Fatalf("folded ID = (%v,%v,%q); want Evaluated expression-looking string", Scalar(name).State(), Scalar(name).Kind(), got)
	}
	if !Scalar(name).Origin().FromExpression {
		t.Fatal("folded ID lost expression origin")
	}
	sourceScalar := name.SourceScalar
	if !sourceScalar.HasSource || !sourceScalar.Evaluated || sourceScalar.Tag != definition.YamlTagStr || sourceScalar.Value != "${{ '${{ runtime }}' }}" {
		t.Fatalf("folded source = %#v; want original expression and evaluated marker", sourceScalar)
	}
}

func TestActionInputSourcesMatchResolvedKeyOrigins(t *testing.T) {
	t.Parallel()
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          "${{ 'script' }}": |
            echo source
      - uses: actions/checkout@v4
        with: ${{ fromJSON('{"script":"echo derived"}') }}
`
	workflow, errors := workflowparser.ParseWorkflow([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	job, ok := Bind("input-source", source, workflow, false, "").Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	var actions []*definition.ExecAction
	for step := range job.Steps() {
		if action, ok := step.Action(); ok {
			actions = append(actions, action)
		}
	}
	if len(actions) != 2 {
		t.Fatalf("action count = %d; want 2", len(actions))
	}
	var literalInput *definition.ActionArgument
	literalNames, _ := InputNames(actions[0])
	for name, candidate := range literalNames {
		if name == "script" {
			literalInput = candidate
		}
	}
	if literalInput == nil || literalInput.Source == nil {
		t.Fatal("literal key expression lost its declared source handle")
	}
	var derivedInput *definition.ActionArgument
	derivedNames, _ := InputNames(actions[1])
	for name, candidate := range derivedNames {
		if name == "script" {
			derivedInput = candidate
		}
	}
	if derivedInput != nil {
		t.Fatal("expression-derived input invented a source handle")
	}
}

func TestPublishedIncompleteEnvironmentLookupStaysDynamic(t *testing.T) {
	t.Parallel()
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      known: present
      ${{ insert }}: ${{ github.event.client_payload }}
    steps:
      - run: echo ok
`
	workflow, errors := workflowparser.ParseWorkflow([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Bind("dynamic-env-test", source, workflow, false, "")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	for _, name := range []string{"known", "unknown"} {
		if got := Environment(job.Env).Lookup(name); got.State() != value.Dynamic {
			t.Errorf("incomplete environment lookup %q = %v; want Dynamic", name, got.State())
		}
	}
}

func TestRunnerSelectionPublishesSemanticShape(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, runsOn string
		wantKind     value.ValueKind
		wantState    value.State
	}{
		{name: "whole expression", runsOn: `${{ fromJSON('["linux","arm64"]') }}`, wantKind: value.Array, wantState: value.Evaluated},
		{name: "labels expression with group", runsOn: "\n      labels: ${{ fromJSON('[\"linux\",\"arm64\"]') }}\n      group: custom", wantKind: value.Object, wantState: value.Literal},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := "on: push\njobs:\n  build:\n    runs-on: " + test.runsOn + "\n    steps:\n      - run: echo ok\n"
			workflow, errors := workflowparser.ParseWorkflow([]byte(source))
			if len(errors) != 0 {
				t.Fatalf("parse source: %v", errors)
			}
			job := Bind(test.name, source, workflow, false, "").Workflow()
			view, ok := job.Job("build")
			if !ok {
				t.Fatal("job view missing")
			}
			selection := RunnerSelection(view.RunsOn)
			if selection.Kind() != test.wantKind || selection.State() != test.wantState {
				t.Fatalf("selection = (%v,%v); want (%v,%v)", selection.State(), selection.Kind(), test.wantState, test.wantKind)
			}
			if test.wantKind == value.Array {
				if selection.Len() != 2 {
					t.Fatalf("whole selection items = %d; want 2", selection.Len())
				}
				return
			}
			labels, ok := selection.Prop("labels")
			if !ok || labels.State() != value.Evaluated || labels.Kind() != value.Array || labels.Len() != 2 {
				t.Fatalf("labels = (%v,%v,%d), %v; want evaluated array of two", labels.State(), labels.Kind(), labels.Len(), ok)
			}
			group, ok := selection.Prop("group")
			if !ok || gotText(group) != "custom" {
				t.Fatalf("group = %q, %v; want custom", gotText(group), ok)
			}
			if _, ok := selection.Prop("labels-expression"); ok {
				t.Fatal("selection has synthetic labels-expression property")
			}
		})
	}
}
