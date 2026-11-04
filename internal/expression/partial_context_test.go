package expression

import "testing"

func TestPartialContextLookup(t *testing.T) {
	lookup := func(name string) (any, bool) { return "Windows", name == "runner.os" }
	for _, tc := range []struct {
		input string
		known bool
		value any
	}{
		{"runner.os == 'Windows'", true, true},
		{"runner['os'] == 'Windows'", true, true},
		{"contains(fromJSON('[\"Linux\"]'), runner.os)", true, false},
		{"runner.arch != 'X64'", false, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			node, err := NewParser().Parse(NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			got, known := ExpressionValueWithLookup(node, lookup)
			if known != tc.known || got != tc.value {
				t.Fatalf("got %v/%v, want %v/%v", got, known, tc.value, tc.known)
			}
			if _, known := ExpressionValue(node, lookup); known {
				t.Fatal("legacy root lookup unexpectedly consumed a property path")
			}
		})
	}
}
