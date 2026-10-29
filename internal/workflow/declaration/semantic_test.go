package declaration

import (
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func TestPublishedSemanticsDoNotFollowLegacyMutation(t *testing.T) {
	name := &workflowlang.String{Value: "before", Pos: &workflowlang.Pos{Line: 2, Col: 4}}
	condition := &workflowlang.String{Value: "${{ true }}", Pos: &workflowlang.Pos{Line: 3, Col: 7}}
	job := &workflowlang.Job{
		Name:   name,
		If:     condition,
		RunsOn: &workflowlang.Runner{Labels: []*workflowlang.String{{Value: "ubuntu-latest"}}},
		Strategy: &workflowlang.Strategy{Matrix: &workflowlang.Matrix{Rows: map[string]*workflowlang.MatrixRow{
			"os": {Name: &workflowlang.String{Value: "os"}, Values: []workflowlang.RawYAMLValue{
				&workflowlang.RawYAMLString{Value: "ubuntu-latest", Tag: workflowlang.YamlTagStr},
			}},
		}}},
		Env: &workflowlang.Env{
			Vars: map[string]*workflowlang.EnvVar{
				"defined":  {Name: &workflowlang.String{Value: "defined"}, Value: &workflowlang.String{Value: "present"}},
				"nullable": {Name: &workflowlang.String{Value: "nullable"}, Value: &workflowlang.String{Value: "${{ fromJSON('null') }}"}},
				"empty":    {Name: &workflowlang.String{Value: "empty"}, Value: &workflowlang.String{Value: ""}},
			},
		},
		Steps: []*workflowlang.Step{{If: condition, Exec: &workflowlang.ExecAction{Inputs: map[string]*workflowlang.Input{
			"greeting": {Name: &workflowlang.String{Value: "greeting"}, Value: &workflowlang.String{Value: "hello"}},
		}}}},
	}
	workflow := &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"build": job}}
	bound := Publish("", workflow, false, "").Bind("semantic-test")
	workflowView := bound.Workflow()
	jobView, ok := workflowView.Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	if got, ok := jobView.Name().Semantic().Text(); !ok || got != "before" {
		t.Fatalf("published name = %q, %v; want before, true", got, ok)
	}
	var step StepView
	for candidate := range jobView.Steps() {
		step = candidate
	}
	if got, ok := step.Condition().Bool(); !ok || !got {
		t.Fatalf("condition = %v, %v; want true", got, ok)
	}
	if !step.Condition().Origin().FromExpression {
		t.Fatal("condition lost expression origin")
	}
	if got, ok := jobView.Env().Mapping().Lookup("defined").Text(); !ok || got != "present" {
		t.Fatalf("published environment value = %q, %v; want present, true", got, ok)
	}
	if state := jobView.Env().Mapping().Lookup("unknown").State(); state != value.Missing {
		t.Fatalf("absent key in complete environment = %v; want Missing", state)
	}
	if got := jobView.Env().Mapping().Lookup("nullable"); got.State() != value.Evaluated || got.Kind() != value.Null {
		t.Fatalf("explicit null environment value = (%v,%v); want Evaluated Null", got.State(), got.Kind())
	}
	if got, ok := jobView.Env().Mapping().Lookup("empty").Text(); !ok || got != "" {
		t.Fatalf("explicit empty environment value = %q, %v; want empty string", got, ok)
	}
	actionView, ok := step.Action()
	if !ok {
		t.Fatal("action view missing")
	}
	if got, ok := actionView.ResolvedInputs().Lookup("greeting").Text(); !ok || got != "hello" {
		t.Fatalf("published action input = %q, %v; want hello, true", got, ok)
	}
	// Mutate the owned legacy syntax node after publication. Semantic access must
	// continue to return the value captured by Publish.
	owned := bound.record.workflow.Jobs["build"]
	owned.Name.Value = "after"
	if got, ok := jobView.Name().Semantic().Text(); !ok || got != "before" {
		t.Fatalf("semantic name changed after legacy mutation: %q, %v", got, ok)
	}
	if got, ok := jobView.RunsOn().Selection().Prop("labels"); !ok || got.Len() != 1 {
		t.Fatalf("runner labels = %v, %v; want one label", got, ok)
	}
	if _, ok := jobView.RunsOn().Selection().Prop("expression"); ok {
		t.Fatal("literal runner selection contains synthetic expression property")
	}
	combinations, complete := jobView.Strategy().MatrixResolution().Combinations(10)
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
	workflow, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Publish(source, workflow, false, "").Bind("condition-test")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	if got, ok := job.If().Semantic().Bool(); !ok || got {
		t.Fatalf("job condition = %v, %v; want false", got, ok)
	}
	if got, ok := job.Snapshot().If().Semantic().Bool(); !ok || got {
		t.Fatalf("snapshot condition = %v, %v; want false", got, ok)
	}
	var parallel StepView
	for step := range job.Steps() {
		parallel = step
	}
	group, ok := parallel.Parallel()
	if !ok {
		t.Fatal("parallel step missing")
	}
	var children []StepView
	for child := range group.Steps() {
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
	workflow, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Publish(source, workflow, false, "").Bind("null-test")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	// Legacy String text stays unchanged while the separate source snapshot keeps the YAML tag.
	if sourceValue := workflow.Jobs["build"].Env.Vars["literal-null"].Value.Value; sourceValue != "null" {
		t.Fatalf("parser literal-null value = %q; want observed text null", sourceValue)
	}
	if got := job.Env().Mapping().Lookup("literal-null"); got.State() != value.Literal || got.Kind() != value.Null {
		t.Fatalf("parsed literal null = (%v,%v); want Literal Null", got.State(), got.Kind())
	}
	if got := job.Env().Mapping().Lookup("evaluated-null"); got.State() != value.Evaluated || got.Kind() != value.Null {
		t.Fatalf("evaluated null = (%v,%v); want Evaluated Null", got.State(), got.Kind())
	}
	if got := job.Env().Mapping().Lookup("empty"); got.State() != value.Literal || got.Kind() != value.String || gotText(got) != "" {
		t.Fatalf("empty string = (%v,%v,%q); want Literal empty String", got.State(), got.Kind(), gotText(got))
	}
	if got := job.Env().Mapping().Lookup("blank"); got.State() != value.Literal || got.Kind() != value.Null {
		t.Fatalf("blank null = (%v,%v); want Literal Null", got.State(), got.Kind())
	}
	var actionView ExecActionView
	for step := range job.Steps() {
		if action, ok := step.Action(); ok {
			actionView = action
		}
	}
	if !actionView.Present() {
		t.Fatal("action view missing")
	}
	for name, wantKind := range map[string]value.Kind{"literal-null": value.Null, "blank": value.Null, "empty": value.String} {
		got := actionView.ResolvedInputs().Lookup(name)
		if got.State() != value.Literal || got.Kind() != wantKind {
			t.Errorf("action input %q = (%v,%v); want Literal %v", name, got.State(), got.Kind(), wantKind)
		}
	}
	if got := actionView.ResolvedInputs().Lookup("missing"); got.State() != value.Missing {
		t.Errorf("omitted action input = %v; want Missing", got.State())
	}
}

func TestPublishedSourceScalarIsPointerFreeSnapshot(t *testing.T) {
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      literal-null: null
    steps:
      - run: echo ok
`
	workflow, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	input := workflow.Jobs["build"].Env.Vars["literal-null"].Value
	bound := Publish(source, workflow, false, "").Bind("source-scalar")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	view, ok := job.Env().VarsAt("literal-null")
	if !ok || !view.Present() {
		t.Fatal("environment variable view missing")
	}
	got := view.Value().SourceScalar()
	if !got.HasSource || got.Kind != workflowlang.SourceScalarScalar || got.Tag != workflowlang.YamlTagNull || got.Value != "null" {
		t.Fatalf("published source scalar = %#v; want original YAML null", got)
	}
	input.SourceScalar.Value = "changed after publication"
	if got = view.Value().SourceScalar(); !got.HasSource || got.Value != "null" {
		t.Fatalf("published source scalar followed input mutation: %#v", got)
	}
	manual := Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{
		"manual": {Name: &workflowlang.String{Value: "manual"}},
	}}, false, "").Bind("manual-source")
	manualJob, ok := manual.Workflow().Job("manual")
	if !ok {
		t.Fatal("manual job view missing")
	}
	if got := manualJob.Name().SourceScalar(); got.HasSource || got.Kind != workflowlang.SourceScalarUnknown {
		t.Fatalf("manual string has source provenance: %#v", got)
	}
}

func TestStaticStringFoldPublishesEvaluatedDataAndSource(t *testing.T) {
	const source = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: ${{ '${{ runtime }}' }}
        run: echo ok
`
	workflow, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Publish(source, workflow, false, "").Bind("static-fold")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	var step StepView
	for candidate := range job.Steps() {
		step = candidate
	}
	name := step.ID()
	if got, ok := name.Semantic().Text(); !ok || got != "${{ runtime }}" || name.Semantic().State() != value.Evaluated {
		t.Fatalf("folded ID = (%v,%v,%q); want Evaluated expression-looking string", name.Semantic().State(), name.Semantic().Kind(), got)
	}
	if !name.Semantic().Origin().FromExpression {
		t.Fatal("folded ID lost expression origin")
	}
	sourceScalar := name.SourceScalar()
	if !sourceScalar.HasSource || !sourceScalar.Evaluated || sourceScalar.Tag != workflowlang.YamlTagStr || sourceScalar.Value != "${{ '${{ runtime }}' }}" {
		t.Fatalf("folded source = %#v; want original expression and evaluated marker", sourceScalar)
	}
}

func TestActionInputSourcesMatchResolvedKeyOrigins(t *testing.T) {
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
	workflow, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	job, ok := Publish(source, workflow, false, "").Bind("input-source").Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	var actions []ExecActionView
	for step := range job.Steps() {
		if action, ok := step.Action(); ok {
			actions = append(actions, action)
		}
	}
	if len(actions) != 2 {
		t.Fatalf("action count = %d; want 2", len(actions))
	}
	var literalInput InputView
	literalNames, _ := actions[0].InputNames()
	for name, candidate := range literalNames {
		if name == "script" {
			literalInput = candidate
		}
	}
	if !literalInput.Present() || !literalInput.Source().Present() {
		t.Fatal("literal key expression lost its declared source handle")
	}
	var derivedInput InputView
	derivedNames, _ := actions[1].InputNames()
	for name, candidate := range derivedNames {
		if name == "script" {
			derivedInput = candidate
		}
	}
	if derivedInput.Present() {
		t.Fatal("expression-derived input invented a source handle")
	}
}

func TestPublishedIncompleteEnvironmentLookupStaysDynamic(t *testing.T) {
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
	workflow, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Publish(source, workflow, false, "").Bind("dynamic-env-test")
	job, ok := bound.Workflow().Job("build")
	if !ok {
		t.Fatal("job view missing")
	}
	for _, name := range []string{"known", "unknown"} {
		if got := job.Env().Mapping().Lookup(name); got.State() != value.Dynamic {
			t.Errorf("incomplete environment lookup %q = %v; want Dynamic", name, got.State())
		}
	}
}

func TestRunnerSelectionPublishesSemanticShape(t *testing.T) {
	for _, test := range []struct {
		name, runsOn string
		wantKind     value.Kind
		wantState    value.State
	}{
		{name: "whole expression", runsOn: `${{ fromJSON('["linux","arm64"]') }}`, wantKind: value.Array, wantState: value.Evaluated},
		{name: "labels expression with group", runsOn: "\n      labels: ${{ fromJSON('[\"linux\",\"arm64\"]') }}\n      group: custom", wantKind: value.Object, wantState: value.Literal},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "on: push\njobs:\n  build:\n    runs-on: " + test.runsOn + "\n    steps:\n      - run: echo ok\n"
			workflow, errors := workflowparser.Parse([]byte(source))
			if len(errors) != 0 {
				t.Fatalf("parse source: %v", errors)
			}
			job := Publish(source, workflow, false, "").Bind(test.name).Workflow()
			view, ok := job.Job("build")
			if !ok {
				t.Fatal("job view missing")
			}
			selection := view.RunsOn().Selection()
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
