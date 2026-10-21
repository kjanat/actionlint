package expressions

import (
	"fmt"
	"maps"
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestInheritedScriptEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, workflowEnv, jobEnv, stepEnv string
		unsafe                             bool
	}{
		{"workflow literal", "{VALUE: safe}", "{OTHER: safe}", "{OTHER: safe}", false},
		{"job literal", "{OTHER: safe}", "{VALUE: safe}", "{OTHER: safe}", false},
		{"step literal overrides unsafe job", "{OTHER: safe}", "{VALUE: 'unsafe value'}", "{VALUE: safe}", false},
		{"job overrides unsafe workflow", "{VALUE: 'unsafe value'}", "{VALUE: safe}", "{OTHER: safe}", false},
		{"unrelated step entry", "{VALUE: safe}", "{OTHER: safe}", "{OTHER: safe}", false},
		{"unknown step shadows workflow", "{VALUE: safe}", "{OTHER: safe}", "{VALUE: '${{ github.ref }}'}", true},
		{"unsafe job shadows workflow", "{VALUE: safe}", "{VALUE: 'unsafe value'}", "{OTHER: safe}", true},
		{"unknown step insertion", "{VALUE: safe}", "{OTHER: safe}", "{'${{ insert }}': '${{ fromJSON(vars.ENV) }}'}", true},
		{"unknown job mapping", "{VALUE: safe}", "'${{ fromJSON(vars.ENV) }}'", "{OTHER: safe}", true},
		{"absent variable", "{OTHER: safe}", "{OTHER: safe}", "{OTHER: safe}", true},
		{"aliased workflow env", "&defaults {VALUE: safe}", "{OTHER: safe}", "*defaults", false},
		{"aliased env lacks variable", "&defaults {OTHER: safe}", "{OTHER: safe}", "*defaults", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("on: push\nenv: %s\njobs:\n  test:\n    runs-on: ubuntu-latest\n    env: %s\n    steps:\n      - env: %s\n        run: echo '${{ env.VALUE }}'\n", tc.workflowEnv, tc.jobEnv, tc.stepEnv)
			workflow, errors := workflowlang.Parse([]byte(source))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			rule := newTestChecker()
			job := workflow.Jobs["test"]
			for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(job.Steps[0]), rule.VisitJobPost(job)} {
				if err != nil {
					t.Fatal(err)
				}
			}
			unsafe := false
			for _, finding := range rule.Findings() {
				unsafe = unsafe || finding.Rule == "script-interpolation"
			}
			if unsafe != tc.unsafe {
				t.Fatalf("unsafe=%t, want %t: %+v", unsafe, tc.unsafe, rule.Findings())
			}
			if rule.jobEnv != nil || rule.scriptEnv != nil {
				t.Fatal("environment scope leaked after visiting job")
			}
		})
	}
}

func TestSafeMatrixScriptReferences(t *testing.T) {
	for _, tc := range []struct {
		name, matrix string
		paths        []string
	}{
		{"literal axis", `{target: [linux, windows]}`, []string{"matrix.target"}},
		{"dangerous alternate", `{target: [linux, 'bad"value']}`, nil},
		{"known include", `{target: [linux], include: [{target: windows, extra: safe}]}`, []string{"matrix.target", "matrix.extra"}},
		{"dangerous include", `{target: [linux], include: [{target: 'bad"value'}]}`, nil},
		{"unknown include", `{target: [linux], include: "${{ fromJSON(inputs.extra) }}"}`, nil},
		{"unknown include entry", `{target: [linux], include: ["${{ fromJSON(inputs.extra) }}"]}`, nil},
		{"unknown axis", `{target: [linux], other: "${{ fromJSON(inputs.extra) }}"}`, nil},
		{"unknown nested value", `{target: [{name: linux, extra: "${{ inputs.extra }}"}]}`, nil},
		{"nested object", `{target: [{name: linux, version: 1}, {name: windows, version: 2}]}`, []string{"matrix.target.name", "matrix.target.version", "matrix.target[*]"}},
		{"nested dangerous alternate", `{target: [{name: linux}, {name: 'bad"value'}]}`, nil},
		{"absent nested member", `{target: [{name: linux}, {version: 1}]}`, []string{"matrix.target.name", "matrix.target.version", "matrix.target[*]"}},
		{"absent member before safe value", `{target: [{version: 1}, {name: linux}]}`, []string{"matrix.target.name", "matrix.target.version", "matrix.target[*]"}},
		{"absent member does not erase unsafe value", `{target: [{name: linux}, {version: 1}, {name: 'bad"value'}]}`, []string{"matrix.target.version"}},
		{"unsafe value before absent member", `{target: [{name: 'bad"value'}, {version: 1}, {name: linux}]}`, []string{"matrix.target.version"}},
		{"absent nested array index", `{target: [[linux], [linux, windows]]}`, []string{"matrix.target[0]", "matrix.target[1]", "matrix.target[*]"}},
		{"unsafe nested array index", `{target: [[linux, 'bad"value'], [linux]]}`, []string{"matrix.target[0]"}},
		{"nested include alternatives", `{include: [{target: {name: linux}}, {target: {version: 1}}]}`, []string{"matrix.target.name", "matrix.target.version", "matrix.target[*]"}},
		{"absent JSON member", `{target: "${{ fromJSON('[{\"name\":\"linux\"},{\"version\":1}]') }}"}`, []string{"matrix.target.name", "matrix.target.version", "matrix.target[*]"}},
		{"JSON case collision cannot prove member safe", `{target: ["${{ fromJSON('{\"name\":\"linux\"}') }}", "${{ fromJSON('{\"name\":\"linux\",\"NAME\":\"bad value\"}') }}"]}`, nil},
		{"dotted key cannot certify another path", `{target: [{"item.name": linux, item: {name: 'bad"value'}}]}`, nil},
		{"dotted axis cannot certify another path", `{"target.name": [linux], target: [{name: 'bad"value'}]}`, nil},
		{"exclude does not manufacture proof", `{target: [linux, 'bad"value'], exclude: [{target: 'bad"value'}]}`, nil},
		{"literal row expression", `{target: "${{ fromJSON('[\"linux\",\"windows\"]') }}"}`, []string{"matrix.target"}},
		{"literal whole matrix", `"${{ fromJSON('{\"target\":[\"linux\",\"windows\"]}') }}"`, []string{"matrix.target"}},
		{"literal include expression", `{include: "${{ fromJSON('[{\"target\":\"linux\"},{\"target\":\"windows\"}]') }}"}`, []string{"matrix.target"}},
		{"literal nested fromJSON", `{target: [{item: "${{ fromJSON('{\"name\":\"linux\"}') }}"}]}`, []string{"matrix.target.item.name", "matrix.target.item[*]"}},
		{"nested array", `{target: [[[linux]]]}`, []string{"matrix.target[0][0]", "matrix.target[0][*]"}},
		{"evaluated expression text stays data", `{target: "${{ fromJSON('[\"\\u0024{{ ''safe'' }}\"]') }}"}`, nil},
		{"known insertion", `{target: [linux], "${{ insert }}": {extra: [safe]}}`, []string{"matrix.target", "matrix.extra"}},
		{"unknown insertion", `{target: [linux], "${{ insert }}": "${{ fromJSON(inputs.extra) }}"}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps:\n      - run: echo hello\n"
			workflow, errors := workflowlang.Parse([]byte(source))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			want := map[string]bool{}
			for _, path := range tc.paths {
				want[path] = true
			}
			if got := safeMatrixScriptReferences(workflow.Jobs["test"].Strategy.Matrix); !maps.Equal(got, want) {
				t.Fatalf("safe references = %v, want %v", got, want)
			}
		})
	}
}

func TestSafeMatrixScriptReferenceIndexes(t *testing.T) {
	proofs := map[string]bool{"matrix.container[*]": true, "matrix.nested[0][0]": true, "matrix.target.name": true}
	for _, tc := range []struct {
		expression string
		want       bool
	}{
		{"matrix.container[matrix.version]", true},
		{"matrix.container['3.10']", true},
		{"matrix.nested[0][0]", true},
		{"matrix.nested[0][inputs.index]", false},
		{"matrix['target']['NAME']", true},
		{"matrix['target.name']", false},
		{"matrix.container[inputs.key].name", false},
	} {
		node, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.expression + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		if got := safeMatrixScriptReference(node, proofs); got != tc.want {
			t.Errorf("%s: safe=%t, want %t", tc.expression, got, tc.want)
		}
	}
}

func TestNestedMatrixScriptInterpolation(t *testing.T) {
	for _, tc := range []struct {
		name, matrix string
		unsafe       bool
	}{
		{"absent member", `{target: [{name: linux}, {version: 1}]}`, false},
		{"absent member first", `{target: [{version: 1}, {name: linux}]}`, false},
		{"unsafe member", `{target: [{name: linux}, {version: 1}, {name: 'bad"value'}]}`, true},
		{"unsafe member first", `{target: [{name: 'bad"value'}, {version: 1}, {name: linux}]}`, true},
		{"include alternatives", `{include: [{target: {name: linux}}, {target: {version: 1}}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps:\n      - run: echo '${{ matrix.target.name }}'\n"
			workflow, errors := workflowlang.Parse([]byte(source))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			rule := newTestChecker()
			job := workflow.Jobs["test"]
			for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(job.Steps[0]), rule.VisitJobPost(job)} {
				if err != nil {
					t.Fatal(err)
				}
			}
			unsafe := false
			for _, finding := range rule.Findings() {
				unsafe = unsafe || finding.Rule == "script-interpolation"
			}
			if unsafe != tc.unsafe {
				t.Fatalf("unsafe=%t, want %t: %+v", unsafe, tc.unsafe, rule.Findings())
			}
		})
	}
}
