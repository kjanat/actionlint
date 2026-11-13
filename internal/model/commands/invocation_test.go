package commands

import "testing"

func TestArgumentParsingDoesNotUseShellQuoting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		want  []string
	}{
		{`-o "pipefail" {0}`, []string{"-o", "pipefail", "{0}"}},
		{`-o 'pipefail' {0}`, []string{"-o", "'pipefail'", "{0}"}},
		{`"a b" "" {0}`, []string{"a b", "", "{0}"}},
		{`"a\\" "b\\\"c"`, []string{`a\`, `b\"c`}},
	} {
		got := Arguments(tc.input, 7)
		if len(got) != len(tc.want) {
			t.Fatalf("%q: %+v", tc.input, got)
		}
		for i, token := range got {
			if token.Value != tc.want[i] || token.Start < 7 || token.End > len(tc.input)+7 {
				t.Fatalf("%q: %+v want %q", tc.input, got, tc.want)
			}
		}
	}
}

func TestAmbiguousAndMissingProvidersAreUnknown(t *testing.T) {
	t.Parallel()
	registry, err := New([]Definition{{ID: "a", Executables: []string{"custom"}}, {ID: "b", Executables: []string{"custom"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.Analyze("custom {0}", false); got.Status != Unknown || got.Reason == "" {
		t.Fatalf("ambiguous: %+v", got)
	}
	if got := registry.Validate("missing", nil); got.Status != Unknown || got.Reason == "" {
		t.Fatalf("missing: %+v", got)
	}
}
