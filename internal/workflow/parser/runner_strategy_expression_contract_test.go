package parser_test

import (
	"encoding/json"
	"reflect"
	"testing"

	matrixlang "actionlint.kjanat.dev/v2/internal/workflow/matrix"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func TestRunnerMatrixFilterExpansion(t *testing.T) {
	// Matrix filters compare leaf values; include entries preserve original axes.
	// https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/Conversion/MatrixBuilder.cs#L509-L730
	for _, tc := range []struct {
		name, matrix string
		want         []string
	}{
		{"object include", `{"config":[{"os":"linux","version":24}],"include":[{"config":{"os":"LINUX"},"label":"ubuntu-latest"}]}`, []string{`{"config": {"os": "linux", "version": 24}, "label": "ubuntu-latest"}`}},
		{"array include", `{"config":[["linux",24]],"include":[{"config":["LINUX"],"label":"ubuntu-latest"}]}`, []string{`{"config": ["linux", 24], "label": "ubuntu-latest"}`}},
		{"object exclude", `{"config":[{"os":"linux","version":24}],"exclude":[{"config":{"os":"LINUX"}}]}`, nil},
		{"array exclude", `{"config":[["linux",24]],"exclude":[{"config":["LINUX"]}]}`, nil},
		{"numeric string include", `{"version":[24],"include":[{"version":"24","label":"ubuntu-latest"}]}`, []string{`{"label": "ubuntu-latest", "version": 24}`}},
		{"numeric string exclude", `{"version":[24],"exclude":[{"version":"24"}]}`, nil},
		{"numeric bool exclude", `{"version":[1],"exclude":[{"version":true}]}`, nil},
		{"hex string exclude", `{"version":[24],"exclude":[{"version":"0x18"}]}`, nil},
		{"octal string exclude", `{"version":[24],"exclude":[{"version":"0o30"}]}`, nil},
		{"signed hex string exclude", `{"version":[-1],"exclude":[{"version":"0xffffffff"}]}`, nil},
		{"signed octal string exclude", `{"version":[-1],"exclude":[{"version":"0o37777777777"}]}`, nil},
		{"trimmed numeric string", `{"version":[24],"exclude":[{"version":" 24 "}]}`, nil},
		{"empty string excludes false", `{"version":[false],"exclude":[{"version":""}]}`, nil},
		{"null excludes zero", `{"version":[0],"exclude":[{"version":null}]}`, nil},
		{"different strings do not coerce", `{"version":["24"],"exclude":[{"version":"024"}]}`, []string{`{"version": "24"}`}},
		{"reject YAML null spelling", `{"version":[0],"exclude":[{"version":"null"}]}`, []string{`{"version": 0}`}},
		{"reject YAML bool spelling", `{"version":[1],"exclude":[{"version":"true"}]}`, []string{`{"version": 1}`}},
		{"reject underscores", `{"version":[24],"exclude":[{"version":"2_4"}]}`, []string{`{"version": 24}`}},
		{"reject uppercase radix prefix", `{"version":[24],"exclude":[{"version":"0X18"}]}`, []string{`{"version": 24}`}},
		{"reject hexadecimal floats", `{"version":[24],"exclude":[{"version":"0x1.8p4"}]}`, []string{`{"version": 24}`}},
		{"missing null leaf", `{"config":[{"os":"linux"}],"exclude":[{"config":{"version":null}}]}`, nil},
		{"array null past last index", `{"config":[["linux"]],"exclude":[{"config":["LINUX",null]}]}`, nil},
		{"empty nested filter", `{"config":[{"os":"linux"}],"exclude":[{"config":{}}]}`, nil},
		{"missing nonnull leaf", `{"config":[{"os":"linux"}],"exclude":[{"config":{"version":24}}]}`, []string{`{"config": {"os": "linux"}}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, expression := range []bool{false, true} {
				strategy := "strategy:\n      matrix: " + tc.matrix
				if expression {
					strategy = "strategy: ${{ fromJSON('{\"matrix\":" + tc.matrix + "}') }}"
				}
				source := `on: push
jobs:
  test:
    ` + strategy + `
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
				workflow, errs := workflowparser.ParseDocument([]byte(source))
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				combinations, known := matrixlang.Resolve(workflow.Jobs["test"].Strategy).Combinations(256)
				if !known || len(combinations) != len(tc.want) {
					t.Fatalf("known=%v, wanted %v, got %v", known, tc.want, combinations)
				}
				for i, combination := range combinations {
					var expected map[string]any
					if err := json.Unmarshal([]byte(tc.want[i]), &expected); err != nil {
						t.Fatal(err)
					}
					actual := map[string]any{}
					for name, item := range combination.Values() {
						actual[name] = matrixValueForTest(item)
					}
					if !reflect.DeepEqual(actual, expected) {
						t.Fatalf("wanted %v, got %v", expected, actual)
					}
				}
			}
		})
	}
}

func TestRunnerMatrixNumericFilters(t *testing.T) {
	for _, tc := range []struct {
		value, filter string
		matches       bool
	}{
		{".inf", "Infinity", true},
		{".inf", "infinity", true},
		{".inf", "+Infinity", true},
		{".inf", "1e999", true},
		{"-.inf", "-1e999", true},
		{".inf", ".inf", false},
		{".inf", "Inf", false},
		{".nan", "NaN", false},
		{"0", "1e-999", true},
	} {
		t.Run(tc.value+"_"+tc.filter, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    strategy:
      matrix: {value: [` + tc.value + "], exclude: [{value: '" + tc.filter + `'}]}
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
			workflow, errs := workflowparser.ParseDocument([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			combinations, known := matrixlang.Resolve(workflow.Jobs["test"].Strategy).Combinations(256)
			if !known || (len(combinations) == 0) != tc.matches {
				t.Fatalf("known=%v, expected exclusion=%v, got %v", known, tc.matches, combinations)
			}
		})
	}
}

func matrixValueForTest(item value.Value) any {
	switch item.Kind() {
	case value.Null:
		return nil
	case value.Bool:
		result, _ := item.Bool()
		return result
	case value.Number:
		result, _ := item.Number()
		return result
	case value.String:
		result, _ := item.Text()
		return result
	case value.Array:
		result := make([]any, item.Len())
		for i := range item.Len() {
			child, _ := item.Index(i)
			result[i] = matrixValueForTest(child)
		}
		return result
	case value.Object:
		result := map[string]any{}
		for name, child := range item.Props() {
			result[name] = matrixValueForTest(child)
		}
		return result
	case value.KindUnknown:
	}
	return nil
}
