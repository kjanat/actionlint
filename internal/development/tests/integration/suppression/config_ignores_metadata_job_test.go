package suppression_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixturefiles"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestReusableIgnoresMetadataPerJob(t *testing.T) {
	t.Parallel()
	for _, metadata := range []string{
		`name: inner
description: test
runs:
  using: composite
  steps:
    - id: inner
      run: echo ok
`,
		`name: inner
description: test
runs:
  using: composite
  steps:
    - id: inner
      run: echo ok
      shell: bash
      timeout-minutes: wrong
`,
	} {
		rule := "shell-name"
		if strings.Contains(metadata, "timeout-minutes") {
			rule = "syntax-check"
		}
		root, _ := testutil.ExecutableFixture(t, "shellcheck")
		fixturefiles.Write(t, root, "inner/action.yml", metadata)
		const source = `on: push
jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      - id: outer
        uses: ./inner
  b:
    runs-on: ubuntu-latest
    steps:
      - id: outer
        uses: ./inner
permissions: {}
`
		for _, tc := range []struct {
			ignores string
			want    int
		}{
			{"[{rule: " + rule + ", job: a, step: inner}]", 1},
			{"[{rule: " + rule + ", job: b, step: inner}]", 1},
			{"[{rule: " + rule + ", job: a, step: inner}, {rule: " + rule + ", job: b, step: inner}]", 0},
			{"[{rule: " + rule + ", job: a, step: outer}, {rule: " + rule + ", job: b, step: outer}]", 1},
		} {
			config, err := checking.ConfigurationRules().ParseConfig([]byte("tools: {shellcheck: false, ruff: false}\nignores: " + tc.ignores))
			if err != nil {
				t.Fatal(err)
			}
			result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.InputFile{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(source), Config: config, Project: workspace.Identity(root)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != tc.want {
				t.Fatalf("ignores %s: %+v", tc.ignores, result.Diagnostics)
			}
			for _, d := range result.Diagnostics {
				if d.Path != filepath.Join("inner", "action.yml") {
					t.Fatalf("lost metadata location: %+v", d)
				}
			}
		}
	}
}
