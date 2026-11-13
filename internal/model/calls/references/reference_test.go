package references

import (
	"testing"
)

func TestActionReferenceProblem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ spec, problem string }{
		{"owner/repo@v1", ""},
		{"owner/repo/path/to/action@refs/heads/master", ""},
		{"owner/repo", "ref is missing"},
		{"repo@v1", "owner is missing"},
		{"/repo@v1", "owner and repo and ref should not be empty"},
		{"owner/@v1", "owner and repo and ref should not be empty"},
		{"owner/repo@", "owner and repo and ref should not be empty"},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			if got := ActionReferenceProblem(tc.spec); got != tc.problem {
				t.Fatalf("problem=%q, want %q", got, tc.problem)
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
