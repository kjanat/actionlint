package actions_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixturefiles"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestGeneratedActionFilesHaveIndependentRules(t *testing.T) {
	t.Parallel()
	// The metadata syntax defines main, pre and post relative to the action directory:
	// https://docs.github.com/en/actions/reference/workflows-and-actions/metadata-syntax
	const manifest = "name: generated\ndescription: generated\ninputs: {known: {description: known}}\nruns: {using: node24, main: dist/main.js, pre: dist/pre.js, post: dist/post.js}\n"
	const workflow = "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/generated\n        with: {unknown: value}\n"
	for _, standalone := range []bool{false, true} {
		for _, test := range []struct {
			name, config  string
			missing       int
			fileSeverity  string
			inputSeverity string
		}{
			{"default", "", 3, "", ""},
			{"file override", "overrides: [{includes: [generated/action.yml], lint: {rules: {correctness: {missing-file: off}}}}]", 0, "", ""},
			{"independent severity", "lint: {rules: {correctness: {missing-file: info, unknown-input: warn}}}", 3, "info", "warning"},
			{"file ignore", "ignores: [{rule: missing-file, file: generated/action.yml}]", 0, "", ""},
			{"reference disabled", "lint: {rules: {correctness: {invalid-action-reference: off}}}", 3, "", ""},
		} {
			t.Run(test.name+map[bool]string{false: "/referenced", true: "/selected"}[standalone], func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				fixturefiles.Write(t, root, "generated/action.yml", manifest)
				config, err := checking.ConfigurationRules().ParseConfig([]byte(test.config))
				if err != nil {
					t.Fatal(err)
				}
				project := workspace.Identity(root)
				sources := []workspace.InputFile{{Path: "ci.yml", Content: []byte(workflow), Project: project, Config: config}}
				if standalone {
					sources = append(sources, workspace.InputFile{Path: "generated/action.yml", Content: []byte(manifest), Project: project, Config: config})
				}
				result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, sources)
				if err != nil {
					t.Fatal(err)
				}
				files, inputs := 0, 0
				for _, diagnostic := range result.Diagnostics {
					switch diagnostic.Rule {
					case "missing-file":
						files++
						if string(diagnostic.Severity) != test.fileSeverity {
							t.Fatalf("file severity changed with input selection: %+v", diagnostic)
						}
					case "unknown-input":
						inputs++
						if diagnostic.Path != "ci.yml" || string(diagnostic.Severity) != test.inputSeverity {
							t.Fatalf("input finding lost its caller or severity: %+v", diagnostic)
						}
					default:
						t.Fatalf("unexpected diagnostic: %+v", diagnostic)
					}
				}
				if files != test.missing || inputs != 1 {
					t.Fatalf("missing files=%d, unknown inputs=%d; expected %d and 1: %+v", files, inputs, test.missing, result.Diagnostics)
				}
			})
		}
	}
}
