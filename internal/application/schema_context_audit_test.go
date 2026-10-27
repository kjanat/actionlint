package application

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/engine"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/visit"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func TestSchemaAuditExpressionVisitorCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, job, step, want string
	}{
		{"snapshot step output", `snapshot:
      image-name: image
      if: steps.build.outputs.ready != '' && success()`, "id: build\n        run: echo test", ""},
		{"snapshot missing step", `snapshot:
      image-name: image
      if: steps.missing.outputs.ready != ''`, "run: echo test", `property "missing"`},
		{"snapshot image invalid context", "snapshot:\n      image-name: ${{ env.IMAGE }}", "run: echo test", `context "env" is not allowed`},
		{"snapshot version invalid context", `snapshot:
      image-name: image
      version: ${{ secrets.VERSION }}`, "run: echo test", `context "secrets" is not allowed`},
		{"snapshot condition secret", `snapshot:
      image-name: image
      if: secrets.READY != ''`, "run: echo test", `context "secrets" is not allowed`},
		{"background bool", "", "run: echo test\n        background: ${{ github.event_name == 'push' }}", ""},
		{"background wrong type", "", "run: echo test\n        background: ${{ 1 }}", "type of expression must be bool"},
		{"background malformed expression", "", "run: echo test\n        background: ${{ broken( }}", "unexpected"},
		{"job status arguments", "if: success('prepare')", "run: echo test", ""},
		{"step status arguments", "", "run: echo test\n        if: success('prepare')", "number of arguments is wrong"},
		{"static shell literal expression", "", "run: echo test\n        shell: ${{ 'bash' }}", ""},
		{"static shell function expression", "", "run: echo test\n        shell: ${{ format('bash') }}", "expressions are not allowed"},
		{"static shell interpolation", "", "run: echo test\n        shell: prefix-${{ 'bash' }}", "expressions are not allowed"},
		{"strategy object matrix inference", `strategy: ${{ fromJSON('{"matrix":{"os":["ubuntu-latest"]},"fail-fast":false}') }}`, "run: echo ${{ matrix.os }}", ""},
		{"strategy mixed case matrix inference", `strategy: ${{ fromJSON('{"Matrix":{"OS":["ubuntu-latest"]}}') }}`, "run: echo ${{ matrix.os }}", ""},
		{"strategy mixed case include inference", `strategy: ${{ fromJSON('{"MATRIX":{"Include":[{"OS":"ubuntu-latest"}]}}') }}`, "run: echo ${{ matrix.os }}", ""},
		{"strategy scalar axis", `strategy: ${{ fromJSON('{"matrix":{"os":"ubuntu-latest"}}') }}`, "run: echo test", "strategy.matrix.os must be array"},
		{"strategy object axis", `strategy: ${{ fromJSON('{"matrix":{"os":{}}}') }}`, "run: echo test", "strategy.matrix.os must be array"},
		{"strategy include scalar", `strategy: ${{ fromJSON('{"matrix":{"include":[1]}}') }}`, "run: echo test", "strategy.matrix.include[] must be object"},
		{"strategy exclude scalar", `strategy: ${{ fromJSON('{"matrix":{"exclude":[true]}}') }}`, "run: echo test", "strategy.matrix.exclude[] must be object"},
		{"strategy heterogeneous include", `strategy: ${{ fromJSON('{"matrix":{"include":[{},1]}}') }}`, "run: echo test", "strategy.matrix.include[1] must be object"},
		{"strategy duplicate matrix", `strategy: ${{ fromJSON('{"matrix":{},"Matrix":{}}') }}`, "run: echo test", `collide under case-insensitive lookup`},
		{"strategy boolean property", `strategy: ${{ fromJSON('{"fail-fast":1}') }}`, "run: echo test", "strategy.fail-fast must be bool"},
		{"strategy zero parallelism", `strategy: ${{ fromJSON('{"max-parallel":0}') }}`, "run: echo test", "must be greater than zero"},
		{"strategy negative parallelism", `strategy: ${{ fromJSON('{"Max-Parallel":-2}') }}`, "run: echo test", "must be greater than zero"},
		{"strategy positive parallelism", `strategy: ${{ fromJSON('{"max-parallel":2}') }}`, "run: echo test", ""},
		{"strategy unknown parallelism", `strategy: ${{ fromJSON(vars.STRATEGY) }}`, "run: echo test", ""},
		{"strategy unknown property", `strategy: ${{ fromJSON('{"failfast":false}') }}`, "run: echo test", "unknown property"},
		{"matrix axis expression inference", `strategy: {matrix: "${{ fromJSON('{\"os\":[\"ubuntu-latest\"]}') }}"}`, "run: echo ${{ matrix.os }}", ""},
		{"defaults object", "defaults:\n      run: ${{ fromJSON('{\"shell\":\"bash\",\"working-directory\":\"src\"}') }}", "run: echo test", ""},
		{"defaults unknown key", "defaults:\n      run: ${{ fromJSON('{\"working-dir\":\"src\"}') }}", "run: echo test", "unknown property"},
		{"defaults empty shell", `defaults: {run: "${{ fromJSON('{\"shell\":\"\"}') }}"}`, "run: echo test", "defaults.run.shell must be a non-empty string"},
		{"defaults null shell", `defaults: {run: "${{ fromJSON('{\"shell\":null}') }}"}`, "run: echo test", "defaults.run.shell must be a non-empty string"},
		{"defaults empty directory", `defaults: {run: "${{ fromJSON('{\"working-directory\":\"\"}') }}"}`, "run: echo test", "defaults.run.working-directory must be a non-empty string"},
		{"defaults null directory", `defaults: {run: "${{ fromJSON('{\"working-directory\":null}') }}"}`, "run: echo test", "defaults.run.working-directory must be a non-empty string"},
		{"container object", `container: ${{ fromJSON('{"image":"node:22","env":{"NUMBER":1},"ports":[80]}') }}`, "run: echo test", ""},
		{"container missing image", `container: ${{ fromJSON('{}') }}`, "run: echo test", `requires property "image"`},
		{"container cased image", `container: ${{ fromJSON('{"Image":"node:22"}') }}`, "run: echo test", ""},
		{"container bad ports", `container: ${{ fromJSON('{"image":"node:22","ports":"80"}') }}`, "run: echo test", "container.ports must be array"},
		{"container expression ports", `container:
      image: node:22
      ports: ${{ fromJSON('[80]') }}`, "run: echo test", ""},
		{"container expression volumes", `container:
      image: node:22
      volumes: ${{ fromJSON('["/data"]') }}`, "run: echo test", ""},
		{"environment object", `environment: ${{ fromJSON('{"name":"staging","deployment":false}') }}`, "run: echo test", ""},
		{"environment empty scalar", `environment: ${{ '' }}`, "run: echo test", "must be a non-empty string"},
		{"environment empty name", `environment: ${{ fromJSON('{"name":""}') }}`, "run: echo test", "must be a non-empty string"},
		{"environment null name", `environment: ${{ fromJSON('{"Name":null}') }}`, "run: echo test", "must be a non-empty string"},
		{"environment valid scalar", `environment: ${{ 'staging' }}`, "run: echo test", ""},
		{"environment unknown", `environment: ${{ vars.ENVIRONMENT }}`, "run: echo test", ""},
		{"environment missing name", `environment: ${{ fromJSON('{"url":"https://example.com"}') }}`, "run: echo test", "requires property"},
		{"concurrency object", `concurrency: ${{ fromJSON('{"group":"build","cancel-in-progress":true}') }}`, "run: echo test", ""},
		{"concurrency bad boolean", `concurrency: ${{ fromJSON('{"group":"build","cancel-in-progress":1}') }}`, "run: echo test", "concurrency.cancel-in-progress must be bool"},
		{"concurrency bad queue", `concurrency: ${{ fromJSON('{"group":"build","queue":"banana"}') }}`, "run: echo test", "concurrency.queue must be single or max"},
		{"concurrency empty group", `concurrency: ${{ fromJSON('{"group":""}') }}`, "run: echo test", "concurrency.group must be a non-empty string"},
		{"concurrency queue conflict", `concurrency: ${{ fromJSON('{"group":"build","queue":"max","cancel-in-progress":true}') }}`, "run: echo test", "cannot be combined"},
		{"concurrency mixed queue conflict", `concurrency: ${{ fromJSON('{"group":"build","Queue":"max","cancel-in-progress":true}') }}`, "run: echo test", "cannot be combined"},
		{"concurrency mixed cancel conflict", `concurrency: ${{ fromJSON('{"group":"build","queue":"max","Cancel-In-Progress":true}') }}`, "run: echo test", "cannot be combined"},
		{"concurrency mixed keys conflict", `concurrency: ${{ fromJSON('{"Group":"build","Queue":"max","Cancel-In-Progress":true}') }}`, "run: echo test", "cannot be combined"},
		{"concurrency mixed keys valid", `concurrency: ${{ fromJSON('{"Group":"build","Queue":"max","Cancel-In-Progress":false}') }}`, "run: echo test", ""},
		{"heterogeneous ports", `container: ${{ fromJSON('{"image":"node","ports":[80,{}]}') }}`, "run: echo test", "container.ports[1] must be non-empty string"},
		{"empty known port", `container: ${{ fromJSON('{"image":"node","ports":[""]}') }}`, "run: echo test", "container.ports[0] must be a non-empty string"},
		{"snapshot object", `snapshot: ${{ fromJSON('{"image-name":"custom","version":"1.*"}') }}`, "run: echo test", ""},
		{"snapshot object missing name", `snapshot: ${{ fromJSON('{"version":"1.*"}') }}`, "run: echo test", "requires property"},
		{"with object", "", "uses: actions/checkout@v6\n        with: ${{ fromJSON('{\"fetch-depth\":1}') }}", ""},
		{"with nonobject", "", "uses: actions/checkout@v6\n        with: ${{ 'wrong' }}", "with must be object"},
		{"with nonscalar property", "", "uses: actions/checkout@v6\n        with: ${{ fromJSON('{\"fetch-depth\":{}}') }}", "with.fetch-depth must be string"},
		{"services object", `services: ${{ fromJSON('{"db":{"image":"redis","command":"redis-server"}}') }}`, "run: echo test", ""},
		{"unknown expression object", "container: ${{ fromJSON(vars.CONTAINER) }}\n    strategy: ${{ fromJSON(vars.STRATEGY) }}", "run: echo test\n        env: {OS: '${{ matrix.os }}'}", ""},
		{"unsafe strategy interpolation", `strategy: ${{ fromJSON('{"matrix":{"os":["$(id)"]}}') }}`, "run: echo ${{ matrix.os }}", "interpolated value"},
		{"dynamic strategy interpolation", `strategy: ${{ fromJSON(vars.STRATEGY) }}`, "run: echo ${{ matrix.os }}", "interpolated value"},
		{"matrix reserved key expression", `strategy:
      matrix:
        axis: [one]
        "${{ case(true, 'exclude', missing) }}": [{axis: one}]`, "run: echo ok", `undefined variable "missing"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    ` + tc.job + `
    steps:
      - ` + tc.step + "\n"
			workflow, rule := schemaAuditExpressionRule(t, source)
			if tc.name == "matrix reserved key expression" {
				matrix := workflow.Jobs["test"].Strategy.Matrix
				if matrix == nil || matrix.Exclude == nil || matrix.Exclude.Name == nil || matrix.Exclude.Name.Value != "${{ case(true, 'exclude', missing) }}" || matrix.Exclude.Evaluated {
					t.Fatalf("matrix reserved key source lost: %#v", matrix)
				}
			}
			visitor := engine.NewVisitor()
			visitor.AddPass(rule)
			if err := visitor.Visit(workflow); err != nil {
				t.Fatal(err)
			}
			errs := rule.Errs()
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("wanted one %q error, got %v", tc.want, errs)
			}
		})
	}
}

func TestSchemaAuditRunnerObjectExpressions(t *testing.T) {
	for _, tc := range []struct {
		value, want string
	}{
		{`fromJSON('{"group":"ubuntu-runners","labels":["linux"]}')`, ""},
		{`fromJSON('{"labels":"ubuntu-latest"}')`, ""},
		{`fromJSON('{"labels":{}}')`, "unsupported expression type"},
		{`fromJSON('{"unknown":"linux"}')`, "unknown property"},
		{`fromJSON(vars.RUNNERS)`, ""},
	} {
		t.Run(tc.value, func(t *testing.T) {
			workflow, rule := schemaAuditExpressionRule(t, `on: push
jobs:
  test:
    runs-on: ${{ `+tc.value+` }}
    steps:
      - run: echo test
`)
			visitor := engine.NewVisitor()
			visitor.AddPass(rule)
			if err := visitor.Visit(workflow); err != nil {
				t.Fatal(err)
			}
			errs := rule.Errs()
			if tc.want == "" && len(errs) != 0 || tc.want != "" && (len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want)) {
				t.Fatalf("wanted %q, got %v", tc.want, errs)
			}
		})
	}
}

func schemaAuditExpressionRule(t *testing.T, source string) (*workflowlang.Workflow, visit.Rule) {
	t.Helper()
	content := []byte(source)
	document := documentparse.Workflow("schema-audit.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("schema-audit.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	runners, err := runnerlang.Prepare(t.Context(), snapshot, nil, runnerapi.Options{}, "")
	if err != nil {
		t.Fatal(err)
	}
	workflow, ok := snapshot.Workflow()
	if !ok {
		t.Fatal("source snapshot has no workflow")
	}
	for _, descriptor := range builtin.Descriptors() {
		if descriptor.Name == "expression" {
			return workflow, descriptor.Build(builtin.Context{SourceSnapshot: snapshot, Runners: runners})
		}
	}
	t.Fatal("expression rule is not registered")
	return nil, nil
}
