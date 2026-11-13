package actions_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
)

// Composite outputs are published through the manifest's value mapping:
// https://docs.github.com/en/actions/reference/workflows-and-actions/metadata-syntax#outputs-for-composite-actions
// These cases promote the saved issue #361 reproductions in
// .review-artifacts/pr343-output-completeness to permanent host regressions.
func TestNestedCompositeOutputReferences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, metadata, reference string
		want                      int
	}{
		{"closed missing", "outputs: {result: {description: result, value: ready}}\nruns: {using: composite, steps: []}\n", "missing", 1},
		{"closed valid", "outputs: {result: {description: result, value: ready}}\nruns: {using: composite, steps: []}\n", "result", 0},
		{"closed omitted", "runs: {using: composite, steps: []}\n", "missing", 1},
		{"node extensible", "outputs: {result: {description: result}}\nruns: {using: node24, main: index.js}\n", "missing", 0},
		{"docker extensible", "outputs: {result: {description: result}}\nruns: {using: docker, image: Dockerfile}\n", "missing", 0},
		{"invalid metadata", "outputs: {result: {description: result, value: ready}}\nruns: {using: unsupported}\n", "missing", 0},
		{"partial metadata", "outputs: invalid\nruns: {using: composite, steps: []}\n", "missing", 0},
		{"missing metadata", "", "missing", 0},
	} {
		for _, standalone := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/standalone=%t", tc.name, standalone), func(t *testing.T) {
				t.Parallel()
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
				if tc.metadata != "" {
					fixturefiles.Write(t, root, "child/action.yml", "name: child\ndescription: child\n"+tc.metadata)
				}
				fixturefiles.Write(t, root, "child/index.js", "")
				fixturefiles.Write(t, root, "child/Dockerfile", "FROM scratch\n")
				content := "name: outer\ndescription: outer\noutputs:\n  result:\n    description: result\n    value: ${{ steps.child.outputs." + tc.reference + " }}\nruns:\n  using: composite\n  steps:\n    - uses: $/child\n      id: child\n    - run: echo ready\n      shell: bash\n      env:\n        VALUE: ${{ steps.child.outputs." + tc.reference + " }}\n"
				action := fixturefiles.Write(t, root, "outer/action.yml", content)
				entry := action
				if !standalone {
					// Repeated calls must start with separate step contexts, and
					// each occurrence must report its declaring action source.
					entry = fixturefiles.Write(t, root, ".github/workflows/test.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/outer\n      - uses: $/outer\npermissions: {}\n")
				}
				result, err := checking.Check(t.Context(), checking.Options{WorkingDir: root}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{entry}})
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Rule != "expression-reference" || !strings.Contains(diagnostic.Message, "steps.child.outputs") {
						continue
					}
					count++
					if !strings.HasSuffix(filepath.ToSlash(diagnostic.Path), "outer/action.yml") || diagnostic.Code != "unknown-property" || diagnostic.Start.Line != 6 && diagnostic.Start.Line != 15 || diagnostic.End.Line != diagnostic.Start.Line {
						t.Fatalf("output finding lost its declaring source: %+v", diagnostic)
					}
				}
				// The host merges repeated findings at one declaration.
				if count != 2*tc.want {
					t.Fatalf("output findings=%d, want %d: %+v", count, 2*tc.want, result.Diagnostics)
				}
			})
		}
	}
}

func TestCompositeOutputReferencesAcrossStepFields(t *testing.T) {
	t.Parallel()
	for _, field := range []struct{ name, step, outputs string }{
		{"environment", "    - run: echo ready\n      shell: bash\n      env:\n        VALUE: ${{ steps.child.outputs.missing }}\n", ""},
		{"folded environment", "    - run: echo ready\n      shell: bash\n      env:\n        VALUE: >-\n          prefix\n          ${{ steps.child.outputs.missing }}\n", ""},
		{"name", "    - run: echo ready\n      shell: bash\n      name: ${{ steps.child.outputs.missing }}\n", ""},
		{"condition", "    - run: echo ready\n      shell: bash\n      if: steps.child.outputs.missing == 'yes'\n", ""},
		{"working directory", "    - run: echo ready\n      shell: bash\n      working-directory: ${{ steps.child.outputs.missing }}\n", ""},
		{"script", "    - run: echo ${{ steps.child.outputs.missing }}\n      shell: bash\n", ""},
		{"expanded action input", "    - uses: $/child\n      with:\n        message: ${{ steps.child.outputs.missing }}\n", ""},
		{"opaque action input", "    - uses: $/node\n      with:\n        message: ${{ steps.child.outputs.missing }}\n", ""},
		{"output value", "", "outputs:\n  result:\n    description: result\n    value: ${{ steps.child.outputs.missing }}\n"},
		{"folded output value", "", "outputs:\n  result:\n    description: result\n    value: >-\n      prefix\n      ${{ steps.child.outputs.missing }}\n"},
	} {
		t.Run(field.name, func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			fixturefiles.Write(t, root, "child/action.yml", "name: child\ndescription: child\ninputs: {message: {description: message}}\noutputs: {result: {description: result, value: ready}}\nruns: {using: composite, steps: []}\n")
			fixturefiles.Write(t, root, "node/action.yml", "name: node\ndescription: node\ninputs: {message: {description: message}}\nruns: {using: node24, main: index.js}\n")
			fixturefiles.Write(t, root, "node/index.js", "")
			content := "name: outer\ndescription: outer\n" + field.outputs + "runs:\n  using: composite\n  steps:\n    - uses: $/child\n      id: child\n" + field.step
			entry := fixturefiles.Write(t, root, "outer/action.yml", content)
			result, err := checking.Check(t.Context(), checking.Options{WorkingDir: root}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{entry}})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule != "expression-reference" || !strings.Contains(diagnostic.Message, "steps.child.outputs") {
					continue
				}
				count++
				if !strings.HasSuffix(filepath.ToSlash(diagnostic.Path), "outer/action.yml") || diagnostic.End.Line != diagnostic.Start.Line || diagnostic.End.Column <= diagnostic.Start.Column {
					t.Fatalf("output finding has no source range: %+v", diagnostic)
				}
				line := []rune(strings.Split(content, "\n")[diagnostic.Start.Line-1])
				if got := string(line[diagnostic.Start.Column-1 : diagnostic.End.Column-1]); got != "steps.child.outputs.missing" {
					t.Fatalf("finding points at %q instead of the output reference: %+v", got, diagnostic)
				}
			}
			if count != 1 {
				t.Fatalf("output findings=%d, want one: %+v", count, result.Diagnostics)
			}
		})
	}
}

func TestCompositeOutputReferencePolicy(t *testing.T) {
	t.Parallel()
	for _, outputValue := range []bool{false, true} {
		for _, tc := range []struct{ name, config, comment, severity string }{
			{"warning", "lint: {rules: {correctness: {expression-reference: warn}}}\n", "", "warning"},
			{"off", "lint: {rules: {correctness: {expression-reference: off}}}\n", "", ""},
			{"inline", "", " # ignore[expression-reference]", ""},
		} {
			t.Run(fmt.Sprintf("%s/output-value=%t", tc.name, outputValue), func(t *testing.T) {
				t.Parallel()
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				fixturefiles.Write(t, root, ".github/actionlint.yaml", tc.config+"tools: {shellcheck: false, ruff: false}\n")
				fixturefiles.Write(t, root, "child/action.yml", "name: child\ndescription: child\nruns: {using: composite, steps: []}\n")
				content := "name: outer\ndescription: outer\n"
				if outputValue {
					content += "outputs:\n  result:\n    description: result\n    value: ${{ steps.child.outputs.missing }}" + tc.comment + "\n"
				}
				content += "runs:\n  using: composite\n  steps:\n    - uses: $/child\n      id: child\n"
				if !outputValue {
					content += "    - run: echo ready\n      shell: bash\n      env:\n        VALUE: ${{ steps.child.outputs.missing }}" + tc.comment + "\n"
				}
				entry := fixturefiles.Write(t, root, "outer/action.yml", content)
				result, err := checking.Check(t.Context(), checking.Options{WorkingDir: root}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{entry}})
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Rule == "unused-suppression" {
						t.Fatalf("output reference suppression was not used: %+v", result.Diagnostics)
					}
					if diagnostic.Rule == "expression-reference" {
						count++
						if tc.severity == "" || diagnostic.Severity != tc.severity {
							t.Fatalf("output reference policy was not applied: %+v", diagnostic)
						}
					}
				}
				if tc.severity != "" && count != 1 || tc.severity == "" && count != 0 {
					t.Fatalf("output reference count=%d: %+v", count, result.Diagnostics)
				}
			})
		}
	}
}

func TestCompositePropertyFindingsHaveOneOwner(t *testing.T) {
	t.Parallel()
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
	entry := fixturefiles.Write(t, root, "outer/action.yml", "name: outer\ndescription: outer\noutputs:\n  result:\n    description: result\n    value: ${{ github.no_such_property }}\nruns:\n  using: composite\n  steps:\n    - run: echo ready\n      shell: bash\n      env:\n        VALUE: ${{ github.no_such_property }}\n")
	result, err := checking.Check(t.Context(), checking.Options{WorkingDir: root}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{entry}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("expected one property finding per field: %+v", result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Rule != "expression-reference" || diagnostic.Code != "unknown-property" || !strings.Contains(diagnostic.Message, "no_such_property") {
			t.Fatalf("property finding has a different owner: %+v", diagnostic)
		}
	}
}
