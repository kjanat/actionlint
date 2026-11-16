package expressions

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"github.com/google/go-cmp/cmp"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestRuleExpressionCheckRawYAMLStringTag(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		what  string
		value string
		tag   string
		want  types.Type
	}{
		{
			what:  "!!bool tagging the YAML 1.1 spelling on",
			value: "on",
			tag:   definition.YamlTagBool,
			want:  types.StringType(),
		},
		{
			what:  "!!bool tagging the YAML 1.1 spelling yes",
			value: "yes",
			tag:   definition.YamlTagBool,
			want:  types.StringType(),
		},
		{
			what:  "!!bool tagging the YAML 1.1 spelling off",
			value: "off",
			tag:   definition.YamlTagBool,
			want:  types.StringType(),
		},
		{
			what:  "!!bool tagging the core schema spelling true",
			value: "true",
			tag:   definition.YamlTagBool,
			want:  types.BoolType(),
		},
		{
			what:  "!!bool tagging the core schema spelling FALSE",
			value: "FALSE",
			tag:   definition.YamlTagBool,
			want:  types.BoolType(),
		},
		{
			what:  "!!null tagging a spelling outside the core schema",
			value: "none",
			tag:   definition.YamlTagNull,
			want:  types.StringType(),
		},
		{
			what:  "!!null tagging the core schema spelling ~",
			value: "~",
			tag:   definition.YamlTagNull,
			want:  types.NullType(),
		},
		{
			what:  "!!null tagging an empty value",
			value: "",
			tag:   definition.YamlTagNull,
			want:  types.NullType(),
		},
		{
			what:  "!!int tagging a binary integer, which the core schema does not have",
			value: "0b10",
			tag:   definition.YamlTagInt,
			want:  types.StringType(),
		},
		{
			what:  "!!int tagging an underscored integer, which the core schema does not have",
			value: "1_000",
			tag:   definition.YamlTagInt,
			want:  types.StringType(),
		},
		{
			what:  "!!int tagging a hexadecimal integer",
			value: "0x1F",
			tag:   definition.YamlTagInt,
			want:  types.NumberType(),
		},
		{
			what:  "!!float tagging an infinity",
			value: ".inf",
			tag:   definition.YamlTagFloat,
			want:  types.NumberType(),
		},
		{
			what:  "!!str tagging text that looks like a number",
			value: "3.10",
			tag:   definition.YamlTagStr,
			want:  types.StringType(),
		},
		{
			what:  "!!str tagging text that looks like a boolean",
			value: "true",
			tag:   definition.YamlTagStr,
			want:  types.StringType(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			rule := newTestChecker()
			have := rule.checkRawYAMLString(ownedRawStringFixture(&definition.RawYAMLString{Value: tc.value, Tag: tc.tag, Position: &source.Position{}}))
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
