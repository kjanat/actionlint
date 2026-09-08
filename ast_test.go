package actionlint

import "testing"

func TestRawYAMLScalarEquals(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		ltag, rtag  string
		want        bool
	}{
		{"string and number", "1", "1", yamlTagStr, yamlTagInt, false},
		{"untagged string", "1", "1", "", yamlTagStr, true},
		{"string case", "Ubuntu", "ubuntu", yamlTagStr, yamlTagStr, false},
		{"equal decimal", "3.10", "3.1", yamlTagFloat, yamlTagFloat, true},
		{"integer and float", "1", "1.0", yamlTagInt, yamlTagFloat, true},
		{"decimal leading zero", "010", "10", yamlTagInt, yamlTagInt, true},
		{"hexadecimal", "0x10", "16", yamlTagInt, yamlTagInt, true},
		{"octal", "0o10", "8", yamlTagInt, yamlTagInt, true},
		{"signed hexadecimal", "0xffffffff", "-1", yamlTagInt, yamlTagInt, true},
		{"maximum signed integer", "0x7fffffff", "2147483647", yamlTagInt, yamlTagInt, true},
		{"minimum signed integer", "0x80000000", "-2147483648", yamlTagInt, yamlTagInt, true},
		{"hexadecimal overflow", "0x100000000", "4294967296", yamlTagInt, yamlTagInt, false},
		{"exponent", "1e2", "100", yamlTagFloat, yamlTagInt, true},
		{"signed zero", "-0.0", "0", yamlTagFloat, yamlTagInt, true},
		{"different numbers", "1", "2", yamlTagInt, yamlTagInt, false},
		{"boolean case", "TRUE", "true", yamlTagBool, yamlTagBool, true},
		{"different booleans", "true", "false", yamlTagBool, yamlTagBool, false},
		{"boolean and string", "true", "true", yamlTagBool, yamlTagStr, false},
		{"boolean and number", "true", "1", yamlTagBool, yamlTagInt, false},
		{"null spellings", "~", "NULL", yamlTagNull, yamlTagNull, true},
		{"empty null", "", "null", yamlTagNull, yamlTagNull, true},
		{"null and string", "null", "null", yamlTagNull, yamlTagStr, false},
		{"positive infinity", "+.inf", ".INF", yamlTagFloat, yamlTagFloat, true},
		{"negative infinity", "-.inf", ".inf", yamlTagFloat, yamlTagFloat, false},
		{"nan spellings", ".nan", ".NaN", yamlTagFloat, yamlTagFloat, true},
		{"non-core boolean", "yes", "yes", yamlTagBool, yamlTagStr, true},
		{"non-core integer", "1_000", "1_000", yamlTagInt, yamlTagStr, true},
		{"non-core binary", "0b10", "2", yamlTagInt, yamlTagInt, false},
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
		{" ${{...}} ", true},
		{`${{ foo == '{"a": {"b": "c"}}' }}`, true}, // edge case
		{"", false},
		{"${}", false},
		{"{{}}", false},
		{"${{", false},
		{"}}", false},
		{"${{ ${{ }}", false},
		{"abc ${{...}}", false},
		{"${{...}} abc", false},
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
			if have := ContainsExpression(tc.input); tc.want != have {
				t.Fatalf("wanted %v but the function returned %v for input %q", tc.want, have, tc.input)
			}
		})
	}
}
