package outputs_test

import (
	"reflect"
	"slices"
	"testing"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/contexts/outputs"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func property(t *testing.T, object exprtypes.ObjectType, path ...string) exprtypes.Type {
	t.Helper()
	value := object.Type()
	for _, name := range path {
		nested, ok := value.Object()
		if !ok {
			t.Fatalf("%v: expected object, got %v", path, value)
		}
		value, ok = nested.Property(name)
		if !ok {
			t.Fatalf("%v: missing %q in %v", path, name, nested)
		}
	}
	return value
}

func TestActionOutputCompletenessDoesNotChangeValueType(t *testing.T) {
	for _, tc := range []struct {
		name     string
		metadata *actionmeta.Metadata
		strict   bool
	}{
		{"unavailable", nil, false},
		{"omitted", &actionmeta.Metadata{}, false},
		{"dynamic", &actionmeta.Metadata{SkipOutputs: true}, false},
		{"declared empty", &actionmeta.Metadata{Outputs: actionmeta.Outputs{}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := outputs.ActionType(actionmeta.NewDefinition(tc.metadata))
			mapped, open := object.Mapped()
			if object.IsStrict() != tc.strict || open == tc.strict || open && mapped.Kind() != exprtypes.KindString {
				t.Fatalf("output names and values were conflated: %v", object)
			}
		})
	}
	definition := actionmeta.NewDefinition(&actionmeta.Metadata{Outputs: actionmeta.Outputs{"value": {Name: "value"}}})
	object := outputs.ActionType(definition)
	if !object.IsStrict() || property(t, object, "value").Kind() != exprtypes.KindString {
		t.Fatal(object)
	}
	step := outputs.StepType(object)
	for _, path := range [][]string{{"outputs", "value"}, {"conclusion"}, {"outcome"}} {
		if property(t, step, path...).Kind() != exprtypes.KindString {
			t.Fatal(step)
		}
	}
}

func outputWorkflow(t *testing.T) declaration.WorkflowView {
	t.Helper()
	const source = `on: push
jobs:
  ancestor:
    runs-on: ubuntu-latest
    steps: [{run: echo ancestor}]
  direct:
    needs: ancestor
    runs-on: ubuntu-latest
    outputs: {value: hello}
    steps: [{run: echo direct}]
  call:
    uses: ./.github/workflows/callee.yml
  Consumer:
    needs: [direct, DIRECT, call, missing, CONSUMER]
    runs-on: ubuntu-latest
    steps: [{run: echo consumer}]
`
	parsed, errs := workflowparser.Parse([]byte(source))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	return declaration.Publish(source, parsed, false, "").Bind("test").Workflow()
}

func job(t *testing.T, workflow declaration.WorkflowView, name string) declaration.JobView {
	t.Helper()
	view, found := workflow.Job(name)
	if !found {
		t.Fatalf("fixture job %q is absent", name)
	}
	return view
}

func TestJobsAndNeedsShareLoadedOutputDeclarations(t *testing.T) {
	workflow := outputWorkflow(t)
	consumer := job(t, workflow, "consumer")
	called := job(t, workflow, "call")
	metadata := &calls.Metadata{Outputs: calls.Outputs{"artifact": {Name: "artifact"}}}
	loaded := map[declaration.JobView]*calls.Metadata{called: metadata}
	dependencies := outputs.DirectDependencies(workflow, consumer)
	for range 2 {
		if names := slices.Collect(func(yield func(string) bool) {
			for name := range dependencies {
				if !yield(name) {
					return
				}
			}
		}); !reflect.DeepEqual(names, []string{"direct", "call"}) {
			t.Fatal(names)
		}
	}
	needs := outputs.NeedsType(workflow, consumer, loaded)
	jobs := outputs.JobsType(workflow, loaded)
	if needs.Len() != 2 || jobs.Len() != 4 {
		t.Fatalf("needs=%v jobs=%v", needs, jobs)
	}
	if !reflect.DeepEqual(property(t, needs, "call"), property(t, jobs, "call")) {
		t.Fatal("job output models diverged")
	}
	for _, context := range []exprtypes.ObjectType{needs, jobs} {
		if property(t, context, "call", "outputs", "artifact").Kind() != exprtypes.KindString || property(t, context, "direct", "outputs", "value").Kind() != exprtypes.KindString {
			t.Fatal(context)
		}
		if property(t, context, "call", "result").Kind() != exprtypes.KindString {
			t.Fatal(context)
		}
	}
	metadata.Outputs["later"] = calls.Output{Name: "later"}
	delete(loaded, called)
	for _, context := range []exprtypes.ObjectType{needs, jobs} {
		object, _ := property(t, context, "call", "outputs").Object()
		if _, found := object.Property("later"); found || !object.IsStrict() {
			t.Fatal("caller mutation changed a published output type")
		}
	}
}

func TestUnknownAndEmptyReusableOutputsAreDifferent(t *testing.T) {
	workflow := outputWorkflow(t)
	called := job(t, workflow, "call")
	for _, known := range []bool{false, true} {
		var loaded map[declaration.JobView]*calls.Metadata
		if known {
			loaded = map[declaration.JobView]*calls.Metadata{called: {}}
		}
		object, _ := property(t, outputs.JobType(called, loaded), "outputs").Object()
		mapped, open := object.Mapped()
		if object.IsStrict() != known || open == known || open && mapped.Kind() != exprtypes.KindString {
			t.Fatalf("known=%t outputs=%v", known, object)
		}
	}
}
