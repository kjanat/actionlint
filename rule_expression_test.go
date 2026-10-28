package actionlint

import (
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRuleExpressionCaseResultInference(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout string
		want    string
	}{
		{"string results", "case(github.ref == 'refs/heads/main', 'production', 'development')", "must be number but found type string"},
		{"number results", "case(github.ref == 'refs/heads/main', 10, 5)", ""},
		{"nested string results", "case(true, case(false, 'a', 'b'), 'c')", "must be number but found type string"},
		{"object property", `case(true, fromJSON('{"timeout":10}'), fromJSON('{"timeout":5}')).timeout`, ""},
		{"missing object property", `case(true, fromJSON('{"timeout":10}'), fromJSON('{"timeout":5}')).missing`, `property "missing" is not defined`},
		{"unknown default", "case(true, 10, fromJSON(vars.TIMEOUT))", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			src := `on: push
env:
  MY_ENV_VAR: |-
    ${{ case(
      github.ref == 'refs/heads/main', 'production',
      github.ref == 'refs/heads/staging', 'staging',
      startsWith(github.ref, 'refs/heads/feature/'), 'development',
      'unknown'
    ) }}
jobs:
  test:
    runs-on: ubuntu-latest
    timeout-minutes: ${{ ` + tc.timeout + ` }}
    steps:
      - run: echo "$MY_ENV_VAR"
`
			errs, err := linter.Lint("test.yaml", []byte(src), nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected diagnostics: %v", errs)
				}
			} else if len(errs) != 1 || errs[0].Kind != "expression" || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("wanted one %q diagnostic, got %v", tc.want, errs)
			}
		})
	}
}

func TestRuleExpressionCheckRawYAMLStringTag(t *testing.T) {
	testCases := []struct {
		what  string
		value string
		tag   string
		want  ExprType
	}{
		{
			what:  "!!bool tagging the YAML 1.1 spelling on",
			value: "on",
			tag:   yamlTagBool,
			want:  StringType{},
		},
		{
			what:  "!!bool tagging the YAML 1.1 spelling yes",
			value: "yes",
			tag:   yamlTagBool,
			want:  StringType{},
		},
		{
			what:  "!!bool tagging the YAML 1.1 spelling off",
			value: "off",
			tag:   yamlTagBool,
			want:  StringType{},
		},
		{
			what:  "!!bool tagging the core schema spelling true",
			value: "true",
			tag:   yamlTagBool,
			want:  BoolType{},
		},
		{
			what:  "!!bool tagging the core schema spelling FALSE",
			value: "FALSE",
			tag:   yamlTagBool,
			want:  BoolType{},
		},
		{
			what:  "!!null tagging a spelling outside the core schema",
			value: "none",
			tag:   yamlTagNull,
			want:  StringType{},
		},
		{
			what:  "!!null tagging the core schema spelling ~",
			value: "~",
			tag:   yamlTagNull,
			want:  NullType{},
		},
		{
			what:  "!!null tagging an empty value",
			value: "",
			tag:   yamlTagNull,
			want:  NullType{},
		},
		{
			what:  "!!int tagging a binary integer, which the core schema does not have",
			value: "0b10",
			tag:   yamlTagInt,
			want:  StringType{},
		},
		{
			what:  "!!int tagging an underscored integer, which the core schema does not have",
			value: "1_000",
			tag:   yamlTagInt,
			want:  StringType{},
		},
		{
			what:  "!!int tagging a hexadecimal integer",
			value: "0x1F",
			tag:   yamlTagInt,
			want:  NumberType{},
		},
		{
			what:  "!!float tagging an infinity",
			value: ".inf",
			tag:   yamlTagFloat,
			want:  NumberType{},
		},
		{
			what:  "!!str tagging text that looks like a number",
			value: "3.10",
			tag:   yamlTagStr,
			want:  StringType{},
		},
		{
			what:  "!!str tagging text that looks like a boolean",
			value: "true",
			tag:   yamlTagStr,
			want:  StringType{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			rule := NewRuleExpression(nil, nil)
			have := rule.checkRawYAMLString(&RawYAMLString{Value: tc.value, Tag: tc.tag, pos: &Pos{}})
			if diff := cmp.Diff(tc.want, have); diff != "" {
				t.Fatalf("wanted %s but got %s\ndiff:\n%s", tc.want.String(), have.String(), diff)
			}
			if errs := rule.Errs(); len(errs) > 0 {
				t.Fatalf("%d error(s) occurred: %v", len(errs), errs)
			}
		})
	}
}
