package expressions

import (
	"fmt"
	"maps"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	matrixcheck "actionlint.kjanat.dev/v2/internal/workflow/matrix"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func TestInheritedScriptEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, workflowEnv, jobEnv, stepEnv string
		unsafe                             bool
	}{
		{"workflow literal can be changed between steps", "{VALUE: safe}", "{OTHER: safe}", "{OTHER: safe}", true},
		{"job literal can be changed between steps", "{OTHER: safe}", "{VALUE: safe}", "{OTHER: safe}", true},
		{"step literal overrides unsafe job", "{OTHER: safe}", "{VALUE: 'unsafe value'}", "{VALUE: safe}", false},
		{"job override can be changed between steps", "{VALUE: 'unsafe value'}", "{VALUE: safe}", "{OTHER: safe}", true},
		{"unrelated step entry does not prove inherited value", "{VALUE: safe}", "{OTHER: safe}", "{OTHER: safe}", true},
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
			rule, workflow := newTestCheckerForSource(t, source)
			job := testJob(t, workflow, "test")
			for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(testStep(t, job, 0)), rule.VisitJobPost(job)} {
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
			if rule.jobEnv.Present() || rule.scriptEnv.Present() {
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
		{"scalar becomes object", `{target: [{name: linux}, {name: {nested: value}}]}`, []string{"matrix.target.name.nested", "matrix.target.name[*]"}},
		{"object becomes scalar", `{target: [{name: {nested: value}}, {name: linux}]}`, []string{"matrix.target.name.nested", "matrix.target.name[*]"}},
		{"scalar becomes array", `{target: [{name: linux}, {name: [value]}]}`, []string{"matrix.target.name[0]", "matrix.target.name[*]"}},
		{"evaluated scalar becomes object", `{target: "${{ fromJSON('[{\"name\":\"linux\"},{\"name\":{\"nested\":\"value\"}}]') }}"}`, []string{"matrix.target.name.nested", "matrix.target.name[*]"}},
		{"evaluated scalar becomes array", `{target: "${{ fromJSON('[{\"name\":\"linux\"},{\"name\":[\"value\"]}]') }}"}`, []string{"matrix.target.name[0]", "matrix.target.name[*]"}},
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
			workflow, errors := workflowparser.Parse([]byte(source))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			want := map[string]bool{}
			for _, path := range tc.paths {
				want[path] = true
			}
			got := map[string]bool{}
			for path, proof := range safeMatrixScriptReferences(matrixcheck.Resolve(workflow.Jobs["test"].Strategy)) {
				got[path] = proof != scriptUnsafe
			}
			if !maps.Equal(got, want) {
				t.Fatalf("safe references = %v, want %v", got, want)
			}
		})
	}
}

func TestWholeStrategyScriptProofUsesCanonicalMatrixResolution(t *testing.T) {
	const source = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy: ${{ fromJSON('{\"matrix\":{\"os\":[\"ubuntu-latest\"]}}') }}\n    steps:\n      - run: echo ${{ matrix.os }}\n"
	content := []byte(source)
	document := documentparse.Workflow("matrix-proof.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("matrix-proof.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	runners, err := runnerlang.Prepare(t.Context(), snapshot, nil, runnerapi.Options{}, "")
	if err != nil {
		t.Fatal(err)
	}
	workflow, ok := snapshot.WorkflowView()
	if !ok {
		t.Fatal("source snapshot has no workflow")
	}
	rule := New(Dependencies{Runners: runners}, Options{})
	if err := rule.VisitWorkflowPre(workflow); err != nil {
		t.Fatal(err)
	}
	job := testJob(t, workflow, "test")
	if err := rule.VisitJobPre(job); err != nil {
		t.Fatal(err)
	}
	if os, _ := rule.matrixTy.Property("os"); os.String() != "string" {
		t.Fatalf("matrix.os type = %s, want string", os)
	}
	if got := rule.matrixScriptSafe["matrix.os"]; got != scriptLiteralWord {
		t.Fatalf("matrix.os script proof = %v, want literal word", got)
	}
	if err := rule.VisitStep(testStep(t, job, 0)); err != nil {
		t.Fatal(err)
	}
	for _, finding := range rule.Findings() {
		if finding.Rule == "script-interpolation" {
			t.Fatalf("known matrix word reported unsafe: %v", finding)
		}
	}
}

func TestSafeMatrixScriptReferenceIndexes(t *testing.T) {
	proofs := map[string]scriptValueProof{
		"matrix.container[*]": scriptArgumentOnly,
		"matrix.nested[0][0]": scriptLiteralWord,
		"matrix.nested[0][*]": scriptArgumentOnly,
		"matrix.target.name":  scriptLiteralWord,
		"matrix.target[*]":    scriptArgumentOnly,
	}
	for _, tc := range []struct {
		expression string
		argument   bool
		want       bool
	}{
		{"matrix.container[matrix.version]", false, false},
		{"matrix.container[matrix.version]", true, true},
		{"matrix.container['missing']", false, false},
		{"matrix.container['missing']", true, true},
		{"matrix.nested[0][9]", false, false},
		{"matrix.nested[0][9]", true, true},
		{"matrix.container['3.10']", false, false},
		{"matrix.nested[0][0]", false, true},
		{"matrix.nested[0][inputs.index]", false, false},
		{"matrix.nested[0][inputs.index]", true, true},
		{"matrix['target']['NAME']", false, true},
		{"matrix['target.name']", false, false},
		{"matrix.container[inputs.key].name", true, false},
		{"matrix.target[matrix.index]", false, false},
		{"matrix.target[matrix.index]", true, true},
	} {
		node, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.expression + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		if got := safeMatrixScriptReference(node, proofs, tc.argument); got != tc.want {
			t.Errorf("%s: safe=%t, want %t", tc.expression, got, tc.want)
		}
	}
}

func TestMissingMatrixPathsAreArgumentOnly(t *testing.T) {
	for _, tc := range []struct {
		name, matrix, path string
		want               scriptValueProof
	}{
		{"optional object member", `{item: [{word: echo}, {}]}`, "matrix.item.word", scriptArgumentOnly},
		{"optional include member", `{include: [{item: {word: echo}}, {item: {}}]}`, "matrix.item.word", scriptArgumentOnly},
		{"omitted include-only axis", `{include: [{word: echo}, {other: safe}]}`, "matrix.word", scriptArgumentOnly},
		{"included axis across base rows", `{target: [linux, windows], include: [{word: echo}]}`, "matrix.word", scriptLiteralWord},
		{"fully covered include-only axis", `{target: [linux, windows], include: [{word: echo}, {word: printf}]}`, "matrix.word", scriptLiteralWord},
		{"filtered include-only axis with base rows", `{target: [linux, windows], include: [{target: linux, word: echo}]}`, "matrix.word", scriptArgumentOnly},
		{"ragged array index", `{target: [[linux], [linux, windows]]}`, "matrix.target[1]", scriptArgumentOnly},
		{"empty object wildcard", `{item: [{}]}`, "matrix.item[*]", scriptArgumentOnly},
		{"empty array wildcard", `{target: [[]]}`, "matrix.target[*]", scriptArgumentOnly},
		{"definite object member", `{item: [{word: echo}, {word: printf}]}`, "matrix.item.word", scriptLiteralWord},
		{"definite array index", `{target: [[linux, windows], [linux, macos]]}`, "matrix.target[1]", scriptLiteralWord},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps:\n      - run: echo hello\n"
			workflow, errors := workflowparser.Parse([]byte(source))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			proofs := safeMatrixScriptReferences(matrixcheck.Resolve(workflow.Jobs["test"].Strategy))
			if got := proofs[tc.path]; got != tc.want {
				t.Fatalf("proof for %s = %v, want %v (all proofs: %v)", tc.path, got, tc.want, proofs)
			}
		})
	}
}

func TestSafeScriptStatusSelectorsRequireKnownMembers(t *testing.T) {
	checker := &Checker{
		stepsTy: new(exprlang.NewObjectType(map[string]exprlang.Type{"producer": exprlang.StringType()})),
		needsTy: new(exprlang.NewObjectType(map[string]exprlang.Type{"build": exprlang.StringType()})),
	}
	for _, tc := range []struct {
		expression string
		want       bool
	}{
		{"steps.producer", true},
		{"steps['PRODUCER']", true},
		{"steps.missing", false},
		{"steps['missing']", false},
		{"steps[inputs.step]", false},
		{"needs.build", true},
		{"needs.missing", false},
		{"needs[inputs.job]", false},
		{"steps.producer.outputs", false},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			node, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			if got := checker.safeScriptStatusSelector(node); got != tc.want {
				t.Fatalf("safe status selector = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestScriptStatusInterpolationUsesKnownMembers(t *testing.T) {
	steps := exprlang.NewObjectType(map[string]exprlang.Type{
		"producer": exprlang.NewObjectType(map[string]exprlang.Type{
			"outcome": exprlang.StringType(),
			"outputs": exprlang.NewObjectType(map[string]exprlang.Type{"value": exprlang.StringType()}).Type(),
		}).Type(),
	})
	needs := exprlang.NewObjectType(map[string]exprlang.Type{
		"build": exprlang.NewObjectType(map[string]exprlang.Type{"result": exprlang.StringType()}).Type(),
	})
	rule := &Checker{stepsTy: &steps, needsTy: &needs}
	for _, tc := range []struct {
		expression string
		wordSafe   bool
		argument   bool
	}{
		{"steps.producer.outcome", true, true},
		{"steps['PRODUCER'].conclusion", true, true},
		{"needs.build.result", true, true},
		{"steps.missing.outcome", false, true},
		{"steps['missing'].outcome", false, true},
		{"steps[inputs.step].outcome", false, true},
		{"needs[inputs.job].result", false, true},
		{"steps.producer.outputs.value", false, false},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			node, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			semantic := exprlang.NewChecker(false, nil)
			semantic.SetContextAvailability([]string{"steps", "needs", "inputs"})
			semantic.UpdateSteps(steps)
			semantic.UpdateNeeds(needs)
			semantic.UpdateInputs(exprlang.NewStrictObjectType(map[string]exprlang.Type{
				"step": exprlang.StringType(), "job": exprlang.StringType(),
			}))
			typeOf, errs := semantic.Check(node)
			if len(errs) != 0 {
				t.Fatalf("expression did not typecheck: %v", errs)
			}
			for _, position := range []struct {
				name string
				pos  exprlang.ScriptPosition
				want bool
			}{
				{"shell word", exprlang.ScriptShellWord, tc.wordSafe},
				{"data argument", exprlang.ScriptShellArgument, tc.argument},
			} {
				findings := semantic.CheckScriptInterpolation(node, typeOf, position.pos, func(node exprlang.Node) bool {
					return rule.safeScriptStatusSelector(node)
				})
				if (len(findings) == 0) != position.want {
					t.Errorf("%s: findings=%v; safe=%t", position.name, findings, position.want)
				}
			}
		})
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
		{"scalar becomes object", `{target: [{name: linux}, {name: {nested: value}}]}`, true},
		{"scalar becomes array", `{target: [{name: linux}, {name: [value]}]}`, true},
		{"include alternatives", `{include: [{target: {name: linux}}, {target: {version: 1}}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps:\n      - run: echo '${{ matrix.target.name }}'\n"
			rule, workflow := newTestCheckerForSource(t, source)
			job := testJob(t, workflow, "test")
			for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(testStep(t, job, 0)), rule.VisitJobPost(job)} {
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
