package actionlint

import (
	"io"
	"testing"
)

func TestSnapshotExpressionVersionGlobs(t *testing.T) {
	for _, tc := range []struct {
		name, field, kind string
		line, column      int
	}{
		{"whole invalid", `snapshot: ${{ fromJSON('{"image-name":"build","version":"["}') }}`, "glob", 5, 15},
		{"whole offset", `snapshot: ${{ fromJSON('{"image-name":"build","version":"version["}') }}`, "glob", 5, 15},
		{"whole mixed case", `snapshot: ${{ fromJSON('{"Image-Name":"build","Version":"["}') }}`, "glob", 5, 15},
		{"whole valid", `snapshot: ${{ fromJSON('{"image-name":"build","version":"1.*"}') }}`, "", 0, 0},
		{"whole omitted", `snapshot: ${{ fromJSON('{"image-name":"build"}') }}`, "", 0, 0},
		{"whole unknown", "snapshot: ${{ fromJSON(vars.SNAPSHOT) }}", "", 0, 0},
		{"whole wrong type", `snapshot: ${{ fromJSON('{"image-name":"build","version":[]}') }}`, "expression", 5, 15},
		{"whole empty", `snapshot: ${{ fromJSON('{"image-name":"build","version":""}') }}`, "expression", 5, 15},
		{"field literal expression", "snapshot:\n      image-name: build\n      version: ${{ '[' }}", "glob", 7, 16},
		{"field JSON expression", "snapshot:\n      image-name: build\n      version: ${{ fromJSON('\"[\"') }}", "glob", 7, 16},
		{"field unknown", "snapshot:\n      image-name: build\n      version: ${{ vars.VERSION }}", "", 0, 0},
		{"field unknown indexed", "snapshot:\n      image-name: build\n      version: ${{ vars['VERSION'] }}", "", 0, 0},
		{"field interpolation", "snapshot:\n      image-name: build\n      version: prefix-${{ vars.VERSION }}", "", 0, 0},
		{"field valid", "snapshot:\n      image-name: build\n      version: ${{ '1.*' }}", "", 0, 0},
		{"field empty", "snapshot:\n      image-name: build\n      version: ${{ '' }}", "expression", 7, 16},
		{"field null", "snapshot:\n      image-name: build\n      version: ${{ null }}", "expression", 7, 16},
		{"empty image name", "snapshot:\n      image-name: ${{ '' }}", "expression", 6, 19},
		{"null image name", "snapshot:\n      image-name: ${{ null }}", "expression", 6, 19},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			source := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    " + tc.field + "\n    steps:\n      - run: echo ok\n"
			errs, err := linter.Lint("workflow.yml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.kind == "" {
				if len(errs) != 0 {
					t.Fatalf("valid or unknown version rejected: %v", errs)
				}
			} else if len(errs) != 1 || errs[0].Kind != tc.kind || errs[0].Line != tc.line || errs[0].Column != tc.column {
				t.Fatalf("want one %s diagnostic at %d:%d, got %v", tc.kind, tc.line, tc.column, errs)
			}
		})
	}
}
