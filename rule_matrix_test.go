package actionlint

import (
	"strings"
	"testing"
)

func TestRuleMatrixScalarValues(t *testing.T) {
	tests := []struct {
		name   string
		matrix string
		want   string
	}{
		{"quoted number", `{value: [1, "1"]}`, ""},
		{"quoted decimal", `{value: [3.10, "3.10"]}`, ""},
		{"equal numbers", `{value: [1, 1.0]}`, "duplicate value"},
		{"numeric spellings", `{value: [10, 1e1]}`, "duplicate value"},
		{"boolean spellings", `{value: [true, True]}`, "duplicate value"},
		{"null spellings", `{value: [null, ~]}`, "duplicate value"},
		{"quoted boolean", `{value: [true, "true"]}`, ""},
		{"quoted null", `{value: [null, "null"]}`, ""},
		{"exclude wrong type", `{value: ["1.0"], exclude: [{value: 1.0}]}`, "does not match"},
		{"exclude same number", `{value: [1], exclude: [{value: 1.0}]}`, ""},
		{"nested wrong type", `{value: [{version: "1"}], exclude: [{value: {version: 1}}]}`, "does not match"},
		{"nested same number", `{value: [[1]], exclude: [{value: [1.0]}]}`, ""},
		{"include value", `{os: [ubuntu-latest], include: [{os: macos-latest}], exclude: [{os: macos-latest}]}`, "does not match"},
		{"include key", `{os: [ubuntu-latest], include: [{gui: gnome}], exclude: [{gui: gnome}]}`, "does not exist"},
		{"include only", `{include: [{os: macos-latest}], exclude: [{os: macos-latest}]}`, "no matrix variation"},
		{"dynamic include", `{os: [ubuntu-latest], include: "${{ fromJSON(inputs.extra) }}", exclude: [{os: macos-latest}]}`, "does not match"},
		{"dynamic include entry", `{os: [ubuntu-latest], include: ["${{ fromJSON(inputs.extra) }}"], exclude: [{os: macos-latest}]}`, "does not match"},
		{"original value", `{os: [ubuntu-latest], include: [{os: macos-latest}], exclude: [{os: ubuntu-latest}]}`, ""},
		{"dynamic row", `{os: "${{ fromJSON(inputs.systems) }}", exclude: [{os: macos-latest}]}`, ""},
		{"dynamic row value", `{os: ["${{ inputs.system }}"], exclude: [{os: macos-latest}]}`, ""},
		{"dynamic exclude", `{os: [ubuntu-latest], exclude: "${{ fromJSON(inputs.excluded) }}"}`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps:\n      - run: echo test\n"
			w, errs := Parse([]byte(src))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			r := NewRuleMatrix()
			v := NewVisitor()
			v.AddPass(r)
			if err := v.Visit(w); err != nil {
				t.Fatal(err)
			}
			errs = r.Errs()
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
