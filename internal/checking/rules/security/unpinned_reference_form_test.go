package security

import (
	"strings"
	"testing"
)

// TestActionUsesRepoRefByForm pins which uses spellings carry a repository ref.
func TestActionUsesRepoRefByForm(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec string
		ref  string
		ok   bool
	}{
		{"", "", false},
		{" ", "", false},
		{"./a", "", false},
		{"./", "", false},
		{"./a@v", "", false},
		{".//a", "", false},
		{"$/a", "", false},
		{"$/a@v", "", false},
		{"$/", "", false},
		{"$//a", "", false},
		{"docker://x", "", false},
		{"docker://", "", false},
		{"docker://x:1", "", false},
		{"docker://x@sha256:ab", "", false},
		{"o/r@v", "v", true},
		{"o/r/p@v", "v", true},
		{"o/r/p/q@v", "v", true},
		{"o/r", "", false},
		{"o/@v", "", false},
		{"/r@v", "", false},
		{"o//p@v", "", false},
		{"o/r@", "", false},
		{"@v", "", false},
		{"o@v", "", false},
		{"builtin:x", "", false},
		{"builtin:", "", false},
		{"builtin:o/r@v", "v", true},
		{"self:o/r@v", "v", true},
		{"self:o/r", "", false},
		{"https://h/o/r@v", "", false},
		{"https://h/o/r/p", "", false},
		{"o/r@v ${{ x }}", "v ${{ x }}", true},
		{" o/r@v", "v", true},
		{"o/r@v@w", "v@w", true},
		{"../a", "", false},
		{"o/../p@v", "v", true},
		{".@v", "", false},
		{"$@v", "", false},
		{"$x/r@v", "v", true},
		{".x/r@v", "v", true},
		{"Docker://x", "", false},
		{"DOCKER://x", "", false},
		{"owner/repo/.github/workflows/ci.yml@v1", "v1", true},
		{"o/r@main", "main", true},
		{"o/r@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", strings.Repeat("a", 40), true},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			ref, ok := actionUsesRepoRef(tc.spec)
			if ref != tc.ref || ok != tc.ok {
				t.Fatalf("actionUsesRepoRef(%q) = %q, %v; want %q, %v", tc.spec, ref, ok, tc.ref, tc.ok)
			}
		})
	}
}
