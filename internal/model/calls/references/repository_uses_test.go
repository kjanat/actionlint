package references

import "testing"

// TestRepositoryUsesAndActionProblem pins how the repository-form parsers treat
// every spelling family, so classification changes cannot move an accept/reject
// boundary of ParseRepositoryUses or ActionReferenceProblem.
func TestRepositoryUsesAndActionProblem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec    string
		ok      bool
		target  RepositoryTarget
		problem string
	}{
		{"", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{" ", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"./a", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"./", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"./a@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{".//a", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"$/a", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"$/a@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{"$/", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"$//a", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"docker://x", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"docker://", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"docker://x:1", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"docker://x@sha256:ab", false, RepositoryTarget{"", "", "", ""}, "owner and repo and ref should not be empty"},
		{"o/r@v", true, RepositoryTarget{"o", "r", "", "v"}, ""},
		{"o/r/p@v", true, RepositoryTarget{"o", "r", "p", "v"}, ""},
		{"o/r/p/q@v", true, RepositoryTarget{"o", "r", "p/q", "v"}, ""},
		{"o/r", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"o/@v", false, RepositoryTarget{"", "", "", ""}, "owner and repo and ref should not be empty"},
		{"/r@v", false, RepositoryTarget{"", "", "", ""}, "owner and repo and ref should not be empty"},
		{"o//p@v", false, RepositoryTarget{"", "", "", ""}, "owner and repo and ref should not be empty"},
		{"o/r@", false, RepositoryTarget{"", "", "", ""}, "owner and repo and ref should not be empty"},
		{"@v", false, RepositoryTarget{"", "", "", ""}, "owner is missing"},
		{"o@v", false, RepositoryTarget{"", "", "", ""}, "owner is missing"},
		{"builtin:x", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"builtin:", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"builtin:o/r@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{"self:o/r@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{"self:o/r", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"https://h/o/r@v", false, RepositoryTarget{"", "", "", ""}, "owner and repo and ref should not be empty"},
		{"https://h/o/r/p", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"http://h:3000/o/r@v", false, RepositoryTarget{"", "", "", ""}, "owner and repo and ref should not be empty"},
		{"o/r@v ${{ x }}", false, RepositoryTarget{"", "", "", ""}, ""},
		{" o/r@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{"o/r@v ", false, RepositoryTarget{"", "", "", ""}, ""},
		{"o/r@v@w", false, RepositoryTarget{"", "", "", ""}, ""},
		{"../a", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"o/../p@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{"o/./p@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{"o/r\\x@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{".@v", false, RepositoryTarget{"", "", "", ""}, "owner is missing"},
		{"$@v", false, RepositoryTarget{"", "", "", ""}, "owner is missing"},
		{"$x/r@v", true, RepositoryTarget{"$x", "r", "", "v"}, ""},
		{".x/r@v", true, RepositoryTarget{".x", "r", "", "v"}, ""},
		{"./a ${{ x }}", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"docker://${{ x }}", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"${{ x }}", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"o/r@${{ x }}", false, RepositoryTarget{"", "", "", ""}, ""},
		{"o/r/${{ x }}@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{"o/r@v/x", true, RepositoryTarget{"o", "r", "", "v/x"}, ""},
		{"Docker://x", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"DOCKER://x", false, RepositoryTarget{"", "", "", ""}, "ref is missing"},
		{"owner/repo/.github/workflows/ci.yml@v1", true, RepositoryTarget{"owner", "repo", ".github/workflows/ci.yml", "v1"}, ""},
		{"o/r/.@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{"o:p/r@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{"o/r\t@v", false, RepositoryTarget{"", "", "", ""}, ""},
		{".git/r@v", true, RepositoryTarget{".git", "r", "", "v"}, ""},
		{"$$/r@v", true, RepositoryTarget{"$$", "r", "", "v"}, ""},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			target, ok := ParseRepositoryUses(tc.spec)
			if ok != tc.ok || target != tc.target {
				t.Errorf("ParseRepositoryUses(%q) = %+v, %v; want %+v, %v", tc.spec, target, ok, tc.target, tc.ok)
			}
			if problem := ActionReferenceProblem(tc.spec); problem != tc.problem {
				t.Errorf("ActionReferenceProblem(%q) = %q; want %q", tc.spec, problem, tc.problem)
			}
		})
	}
}
