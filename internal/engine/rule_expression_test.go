package engine

import (
	"fmt"
	"strings"
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"github.com/google/go-cmp/cmp"
)

func TestRuleExpressionBareFunctionCondition(t *testing.T) {
	for _, location := range []string{"job", "step"} {
		for _, condition := range []string{"always", "${{ always }}", "always()", "unrelated"} {
			t.Run(location+"/"+condition, func(t *testing.T) {
				job, step := "", ""
				line, column := 4, 9
				if location == "job" {
					job = "    if: " + condition + "\n"
				} else {
					step = "        if: " + condition + "\n"
					line, column = 7, 13
				}
				if strings.HasPrefix(condition, "${{") {
					column += 4
				}
				src := fmt.Sprintf("on: push\njobs:\n  test:\n%s    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n%s", job, step)
				result, err := Analyze(t.Context(), AnalysisRequest{
					WorkingDir: t.TempDir(),
					Sources:    []SourceUnit{{Path: "ci.yml", Content: []byte(src)}},
				})
				if err != nil {
					t.Fatal(err)
				}
				if condition == "always()" {
					if len(result.Diagnostics) != 0 {
						t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
					}
					return
				}
				want := `function "always" must be called with parentheses; did you mean "always()"?`
				if condition == "unrelated" {
					want = `undefined variable "unrelated". available variables are `
				}
				if len(result.Diagnostics) != 1 {
					t.Fatalf("want one diagnostic, got %+v", result.Diagnostics)
				}
				d := result.Diagnostics[0]
				if d.Rule != "expression" || !strings.HasPrefix(d.Message, want) || d.Start.Line != line || d.Start.Column != column {
					t.Fatalf("unexpected diagnostic: %+v", d)
				}
			})
		}
	}
}

func TestRuleExpressionCheckRawYAMLStringTag(t *testing.T) {
	testCases := []struct {
		what  string
		value string
		tag   string
		want  exprlang.Type
	}{
		{
			what:  "!!bool tagging the YAML 1.1 spelling on",
			value: "on",
			tag:   workflowlang.YamlTagBool,
			want:  exprlang.StringType{},
		},
		{
			what:  "!!bool tagging the YAML 1.1 spelling yes",
			value: "yes",
			tag:   workflowlang.YamlTagBool,
			want:  exprlang.StringType{},
		},
		{
			what:  "!!bool tagging the YAML 1.1 spelling off",
			value: "off",
			tag:   workflowlang.YamlTagBool,
			want:  exprlang.StringType{},
		},
		{
			what:  "!!bool tagging the core schema spelling true",
			value: "true",
			tag:   workflowlang.YamlTagBool,
			want:  exprlang.BoolType{},
		},
		{
			what:  "!!bool tagging the core schema spelling FALSE",
			value: "FALSE",
			tag:   workflowlang.YamlTagBool,
			want:  exprlang.BoolType{},
		},
		{
			what:  "!!null tagging a spelling outside the core schema",
			value: "none",
			tag:   workflowlang.YamlTagNull,
			want:  exprlang.StringType{},
		},
		{
			what:  "!!null tagging the core schema spelling ~",
			value: "~",
			tag:   workflowlang.YamlTagNull,
			want:  exprlang.NullType{},
		},
		{
			what:  "!!null tagging an empty value",
			value: "",
			tag:   workflowlang.YamlTagNull,
			want:  exprlang.NullType{},
		},
		{
			what:  "!!int tagging a binary integer, which the core schema does not have",
			value: "0b10",
			tag:   workflowlang.YamlTagInt,
			want:  exprlang.StringType{},
		},
		{
			what:  "!!int tagging an underscored integer, which the core schema does not have",
			value: "1_000",
			tag:   workflowlang.YamlTagInt,
			want:  exprlang.StringType{},
		},
		{
			what:  "!!int tagging a hexadecimal integer",
			value: "0x1F",
			tag:   workflowlang.YamlTagInt,
			want:  exprlang.NumberType{},
		},
		{
			what:  "!!float tagging an infinity",
			value: ".inf",
			tag:   workflowlang.YamlTagFloat,
			want:  exprlang.NumberType{},
		},
		{
			what:  "!!str tagging text that looks like a number",
			value: "3.10",
			tag:   workflowlang.YamlTagStr,
			want:  exprlang.StringType{},
		},
		{
			what:  "!!str tagging text that looks like a boolean",
			value: "true",
			tag:   workflowlang.YamlTagStr,
			want:  exprlang.StringType{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			rule := NewRuleExpression(nil, nil)
			have := rule.checkRawYAMLString(&workflowlang.RawYAMLString{Value: tc.value, Tag: tc.tag, Position: &workflowlang.Pos{}})
			if diff := cmp.Diff(tc.want, have); diff != "" {
				t.Fatalf(`wanted %s but got %s
diff:
%s`, tc.want.String(), have.String(), diff)
			}
			if errs := rule.Errs(); len(errs) > 0 {
				t.Fatalf("%d error(s) occurred: %v", len(errs), errs)
			}
		})
	}
}
