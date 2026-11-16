package callreferences

import (
	"testing"
)

func TestParseRepositoryUses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec   string
		target RepositoryTarget
		ok     bool
	}{
		{"o/r@v", RepositoryTarget{"o", "r", "", "v"}, true},
		{"o/r/p/q@v", RepositoryTarget{"o", "r", "p/q", "v"}, true},
		{"o/r@v/x", RepositoryTarget{"o", "r", "", "v/x"}, true},
		{"$x/r@v", RepositoryTarget{"$x", "r", "", "v"}, true},
		{".x/r@v", RepositoryTarget{".x", "r", "", "v"}, true},
		{"./a@v", RepositoryTarget{}, false},
		{"$/a@v", RepositoryTarget{}, false},
		{"docker://x@sha256:ab", RepositoryTarget{}, false},
		{"builtin:o/r@v", RepositoryTarget{}, false},
		{"self:o/r@v", RepositoryTarget{}, false},
		{"https://h/o/r@v", RepositoryTarget{}, false},
		{"o/r", RepositoryTarget{}, false},
		{"o/r@", RepositoryTarget{}, false},
		{"o/r@v ${{ x }}", RepositoryTarget{}, false},
		{" o/r@v", RepositoryTarget{}, false},
		{"o/../p@v", RepositoryTarget{}, false},
		{"o/r@v@w", RepositoryTarget{}, false},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			target, ok := ParseRepositoryUses(tc.spec)
			if ok != tc.ok || target != tc.target {
				t.Fatalf("ParseRepositoryUses(%q) = %+v, %v; want %+v, %v", tc.spec, target, ok, tc.target, tc.ok)
			}
		})
	}
}

func TestRuleActionSelfRepositoryUsesLocalSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		spec string
		want string
		ok   bool
	}{
		{"$/a", "./a", true},
		{"$/path/to/action", "./path/to/action", true},
		{"$//a", "./a", true},
		{"$///a", "./a", true},
		{"$/", "./", true},
		{"$//", "./", true},
		{"$///", "./", true},
		{"./a", "", false},
		{"$", "", false},
		{"", "", false},
		{"owner/repo@v1", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			have, ok := SelfRepositoryLocal(tc.spec)
			if ok != tc.ok {
				t.Fatalf("wanted ok=%v but have ok=%v for %q", tc.ok, ok, tc.spec)
			}
			if have != tc.want {
				t.Fatalf("wanted %q but have %q for %q", tc.want, have, tc.spec)
			}
		})
	}
}
