package actionlint

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

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
