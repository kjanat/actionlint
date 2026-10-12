package actionlint

import (
	"path/filepath"
	"testing"
)

func TestReusableIgnoresMetadataPerJob(t *testing.T) {
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
		root, _ := executableFixture(t)
		writeShellcheckFixture(t, root, "inner/action.yml", metadata)
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
`
		for _, tc := range []struct {
			ignores string
			want    int
		}{
			{"[{rule: action, job: a, step: inner}]", 1},
			{"[{rule: action, job: b, step: inner}]", 1},
			{"[{rule: action, job: a, step: inner}, {rule: action, job: b, step: inner}]", 0},
			{"[{rule: action, job: a, step: outer}, {rule: action, job: b, step: outer}]", 1},
		} {
			config, err := ParseConfig([]byte("tools: {shellcheck: false, ruff: false}\nignores: " + tc.ignores))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Analyze(t.Context(), AnalysisRequest{
				WorkingDir: root,
				Sources:    []SourceUnit{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(source), Config: config, Project: &Project{root: root}}},
			})
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
