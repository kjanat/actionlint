package workflow

import (
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
)

func TestRawYAMLScalarEquals(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		ltag, rtag  string
		want        bool
	}{
		{"string and number", "1", "1", YamlTagStr, YamlTagInt, false},
		{"untagged string", "1", "1", "", YamlTagStr, true},
		{"string case", "Ubuntu", "ubuntu", YamlTagStr, YamlTagStr, false},
		{"equal decimal", "3.10", "3.1", YamlTagFloat, YamlTagFloat, true},
		{"integer and float", "1", "1.0", YamlTagInt, YamlTagFloat, true},
		{"decimal leading zero", "010", "10", YamlTagInt, YamlTagInt, true},
		{"hexadecimal", "0x10", "16", YamlTagInt, YamlTagInt, true},
		{"octal", "0o10", "8", YamlTagInt, YamlTagInt, true},
		{"signed hexadecimal", "0xffffffff", "-1", YamlTagInt, YamlTagInt, true},
		{"maximum signed integer", "0x7fffffff", "2147483647", YamlTagInt, YamlTagInt, true},
		{"minimum signed integer", "0x80000000", "-2147483648", YamlTagInt, YamlTagInt, true},
		{"hexadecimal overflow", "0x100000000", "4294967296", YamlTagInt, YamlTagInt, false},
		{"exponent", "1e2", "100", YamlTagFloat, YamlTagInt, true},
		{"signed zero", "-0.0", "0", YamlTagFloat, YamlTagInt, true},
		{"different numbers", "1", "2", YamlTagInt, YamlTagInt, false},
		{"boolean case", "TRUE", "true", YamlTagBool, YamlTagBool, true},
		{"different booleans", "true", "false", YamlTagBool, YamlTagBool, false},
		{"boolean and string", "true", "true", YamlTagBool, YamlTagStr, false},
		{"boolean and number", "true", "1", YamlTagBool, YamlTagInt, false},
		{"null spellings", "~", "NULL", YamlTagNull, YamlTagNull, true},
		{"empty null", "", "null", YamlTagNull, YamlTagNull, true},
		{"null and string", "null", "null", YamlTagNull, YamlTagStr, false},
		{"positive infinity", "+.inf", ".INF", YamlTagFloat, YamlTagFloat, true},
		{"negative infinity", "-.inf", ".inf", YamlTagFloat, YamlTagFloat, false},
		{"nan spellings", ".nan", ".NaN", YamlTagFloat, YamlTagFloat, true},
		{"non-core boolean", "yes", "yes", YamlTagBool, YamlTagStr, true},
		{"non-core integer", "1_000", "1_000", YamlTagInt, YamlTagStr, true},
		{"non-core binary", "0b10", "2", YamlTagInt, YamlTagInt, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			left := &RawYAMLString{Value: tc.left, Tag: tc.ltag}
			right := &RawYAMLString{Value: tc.right, Tag: tc.rtag}
			if got := left.Equals(right); got != tc.want {
				t.Fatalf("%s %q equals %s %q: got %v, want %v", tc.ltag, tc.left, tc.rtag, tc.right, got, tc.want)
			}
			if got := right.Equals(left); got != tc.want {
				t.Fatalf("reversed equality: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStringIsExpressionAssigned(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"${{...}}", true},
		{" ${{...}} ", false},
		{"${{...}}\n", false},
		{"\t${{...}}", false},
		{"${{ ... }}", true},
		{`${{ foo == '{"a": {"b": "c"}}' }}`, true}, // edge case
		{`${{ fromJSON('{"matrix":{"os":["${{ vars.RUNNER }}"]}}') }}`, true},
		{`${{ '}}' }}`, true},
		{`${{ 'it''s ${{ data }}' }}`, true},
		{`${{ 'unfinished }}`, true},
		{"", false},
		{"${}", false},
		{"{{}}", false},
		{"${{", false},
		{"}}", false},
		{"${{ ${{ }}", false},
		{"abc ${{...}}", false},
		{"${{...}} abc", false},
		{`${{ '}}' }}suffix}}`, false},
		{"${{...}}${{...}}", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			s := &String{Value: tc.input}
			have := s.IsExpressionAssigned()
			if tc.want != have {
				t.Fatalf("wanted %v but got %v for input %q", tc.want, have, tc.input)
			}
		})
	}
}

func TestStringContainsExpression(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"${{...}}", true},
		{"foo-${{...}}-bar", true},
		{"${{...}}-${{...}}", true},
		{"${{...", false},
		{"...}}", false},
		{"${{...} }", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			s := &String{Value: tc.input}
			if have := s.ContainsExpression(); tc.want != have {
				t.Fatalf("wanted %v but the method returned %v for input %q", tc.want, have, tc.input)
			}
			if have := exprlang.ContainsExpression(tc.input); tc.want != have {
				t.Fatalf("wanted %v but the function returned %v for input %q", tc.want, have, tc.input)
			}
		})
	}
}
