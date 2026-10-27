package expressions

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

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
			rule := newTestChecker()
			have := rule.checkRawYAMLString(ownedRawStringFixture(&workflowlang.RawYAMLString{Value: tc.value, Tag: tc.tag, Position: &workflowlang.Pos{}}))
			if diff := cmp.Diff(tc.want, have); diff != "" {
				t.Fatalf(`wanted %s but got %s
diff:
%s`, tc.want.String(), have.String(), diff)
			}
			if errs := rule.Findings(); len(errs) > 0 {
				t.Fatalf("%d error(s) occurred: %v", len(errs), errs)
			}
		})
	}
}
