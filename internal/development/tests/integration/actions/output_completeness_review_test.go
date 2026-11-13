package actions_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

func TestScriptActionUndeclaredOutputAndCompositeClosure(t *testing.T) {
	t.Parallel()
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref, err := actions.ParseReference("github.com", "actions/github-script@"+sha)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity(ref.Host(), ref.Repository(), repositoryid.ID{}, sha, "")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(ref, identity, sha, actions.ObjectRoute(), "recorded-test-fixture")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := actions.NewManifest(selection, []byte("name: github script\ndescription: fixture\ninputs: {script: {description: script}}\noutputs: {result: {description: result}}\nruns: {using: node24, main: dist/index.js}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, expression, rule string
		wantReference          int
	}{
		{"undeclared string output", "steps.script.outputs.token", "expression-reference", 0},
		{"undeclared output is not an object", "steps.script.outputs.token.child", "expression-type", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := fmt.Sprintf("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/github-script@%s\n        id: script\n        with:\n          script: |\n            const token = 'value';\n            core.setOutput('token', token);\n      - run: echo ok\n        env:\n          TOKEN: ${{ %s }}\n", sha, tc.expression)
			findings := testutil.WorkflowDiagnosticsWithOptions(t, source, "tools: {shellcheck: false, ruff: false}", checking.Options{RemoteActions: pinnedMetadataFixture{manifest}})
			count := 0
			for _, finding := range findings {
				if finding.Rule == tc.rule {
					count++
				}
			}
			if count != tc.wantReference {
				t.Fatalf("reference findings=%d, want %d: %+v", count, tc.wantReference, findings)
			}
		})
	}
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("local node nested=%t", nested), func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			fixturefiles.Write(t, root, "node/action.yml", "name: node\ndescription: test\noutputs: {result: {description: result}}\nruns: {using: node24, main: index.js}\n")
			steps := "- uses: ./node\n  id: script\n- run: echo ok\n  env:\n    VALUE: ${{ steps.script.outputs.token }}"
			if nested {
				fixturefiles.Write(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: $/node\n      id: script\n    - run: echo ok\n      shell: bash\n      env:\n        VALUE: ${{ steps.script.outputs.token }}\n")
				steps = "- uses: ./outer"
			}
			result := testutil.Composite(t, root, steps, checking.Options{})
			for _, finding := range result.Diagnostics {
				if finding.Rule == "expression-reference" {
					t.Fatalf("undeclared local node output was rejected: %+v", result.Diagnostics)
				}
			}
		})
	}
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("composite nested=%t", nested), func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			fixturefiles.Write(t, root, "closed/action.yml", "name: closed\ndescription: test\noutputs: {result: {description: result, value: ready}}\nruns: {using: composite, steps: []}\n")
			steps := "- uses: ./closed\n  id: closed\n- run: echo ok\n  env:\n    VALUE: ${{ steps.closed.outputs.missing }}"
			if nested {
				fixturefiles.Write(t, root, "outer/action.yml", "name: outer\ndescription: test\noutputs: {result: {description: result, value: ready}}\nruns:\n  using: composite\n  steps:\n    - uses: $/closed\n      id: closed\n")
				steps = "- uses: ./outer\n  id: closed\n- run: echo ok\n  env:\n    VALUE: ${{ steps.closed.outputs.missing }}"
			}
			result := testutil.Composite(t, root, steps, checking.Options{})
			count := 0
			for _, finding := range result.Diagnostics {
				if finding.Rule == "expression-reference" && strings.Contains(finding.Message, "missing") {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("expected one closed-composite output finding: %+v", result.Diagnostics)
			}
		})
	}
}

func TestRemoteCompositeOutputMappingIsClosed(t *testing.T) {
	t.Parallel()
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ref, err := actions.ParseReference("github.com", "example/composite@"+sha)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity(ref.Host(), ref.Repository(), repositoryid.ID{}, sha, "")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(ref, identity, sha, actions.ObjectRoute(), "recorded-test-fixture")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := actions.NewManifest(selection, []byte("name: composite\ndescription: fixture\noutputs: {result: {description: result, value: ready}}\nruns: {using: composite, steps: []}\n"))
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/composite@%s\n        id: step\n      - run: echo ok\n        env:\n          VALID: ${{ steps.step.outputs.result }}\n          INVALID: ${{ steps.step.outputs.missing }}\n", sha)
	findings := testutil.WorkflowDiagnosticsWithOptions(t, source, "tools: {shellcheck: false, ruff: false}", checking.Options{RemoteActions: pinnedMetadataFixture{manifest}})
	count := 0
	for _, finding := range findings {
		if finding.Rule == "expression-reference" && strings.Contains(finding.Message, "missing") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one remote composite output finding: %+v", findings)
	}
}
