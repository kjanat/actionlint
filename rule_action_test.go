package actionlint

import "testing"

func TestRuleActionSelfRepositoryUsesLocalSpec(t *testing.T) {
	tests := []struct {
		spec string
		want string
		ok   bool
	}{
		{"$/a", "./a", true},
		{"$/path/to/action", "./path/to/action", true},
		{"$//a", "./a", true},
		{"$///a", "./a", true},
		{"$/", "", false},
		{"$//", "", false},
		{"$///", "", false},
		{"./a", "", false},
		{"$", "", false},
		{"", "", false},
		{"owner/repo@v1", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			have, ok := selfRepositoryUsesLocalSpec(tc.spec)
			if ok != tc.ok {
				t.Fatalf("wanted ok=%v but have ok=%v for %q", tc.ok, ok, tc.spec)
			}
			if have != tc.want {
				t.Fatalf("wanted %q but have %q for %q", tc.want, have, tc.spec)
			}
		})
	}
}
