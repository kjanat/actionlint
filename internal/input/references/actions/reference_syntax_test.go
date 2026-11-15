package actions_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
)

// TestParseReferenceSyntax pins which uses spellings ParseReference accepts and
// the parts it extracts, independent of how callers classify a uses value.
func TestParseReferenceSyntax(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ spec, host, repository, directory, ref, err string }{
		{"", "", "", "", "", "action ref is missing"},
		{" ", "", "", "", "", "action ref is missing"},
		{"./a", "", "", "", "", "action ref is missing"},
		{"./", "", "", "", "", "action ref is missing"},
		{"./a@v", "", "", "", "", "action needs an owner and repository"},
		{".//a", "", "", "", "", "action ref is missing"},
		{"$/a", "", "", "", "", "action ref is missing"},
		{"$/a@v", "", "", "", "", "action needs an owner and repository"},
		{"$/", "", "", "", "", "action ref is missing"},
		{"$//a", "", "", "", "", "action ref is missing"},
		{"docker://x", "", "", "", "", "action ref is missing"},
		{"docker://", "", "", "", "", "action ref is missing"},
		{"docker://x:1", "", "", "", "", "action ref is missing"},
		{"docker://x@sha256:ab", "", "", "", "", "action needs an owner and repository"},
		{"o/r@v", "github.com", "o/r", "", "v", ""},
		{"o/r/p@v", "github.com", "o/r", "p", "v", ""},
		{"o/r/p/q@v", "github.com", "o/r", "p/q", "v", ""},
		{"o/r", "", "", "", "", "action ref is missing"},
		{"o/@v", "", "", "", "", "action needs an owner and repository"},
		{"/r@v", "", "", "", "", "action needs an owner and repository"},
		{"o//p@v", "", "", "", "", "action needs an owner and repository"},
		{"o/r@", "", "", "", "", "action ref is missing"},
		{"@v", "", "", "", "", "action needs an owner and repository"},
		{"o@v", "", "", "", "", "action needs an owner and repository"},
		{"builtin:x", "", "", "", "", "action ref is missing"},
		{"builtin:", "", "", "", "", "action ref is missing"},
		{"builtin:o/r@v", "github.com", "builtin:o/r", "", "v", ""},
		{"self:o/r@v", "github.com", "self:o/r", "", "v", ""},
		{"self:o/r", "", "", "", "", "action ref is missing"},
		{"https://h/o/r@v", "", "", "", "", "action needs an owner and repository"},
		{"https://h/o/r/p", "", "", "", "", "action ref is missing"},
		{"http://h:3000/o/r@v", "", "", "", "", "action needs an owner and repository"},
		{"o/r@v ${{ x }}", "github.com", "o/r", "", "v ${{ x }}", ""},
		{" o/r@v", "github.com", " o/r", "", "v", ""},
		{"o/r@v ", "github.com", "o/r", "", "v ", ""},
		{"o/r@v@w", "github.com", "o/r", "", "v@w", ""},
		{"../a", "", "", "", "", "action ref is missing"},
		{"o/../p@v", "github.com", "o/..", "p", "v", ""},
		{"o/./p@v", "github.com", "o/.", "p", "v", ""},
		{"o/r\\x@v", "github.com", "o/r\\x", "", "v", ""},
		{".@v", "", "", "", "", "action needs an owner and repository"},
		{"$@v", "", "", "", "", "action needs an owner and repository"},
		{"$x/r@v", "github.com", "$x/r", "", "v", ""},
		{".x/r@v", "github.com", ".x/r", "", "v", ""},
		{"./a ${{ x }}", "", "", "", "", "action ref is missing"},
		{"docker://${{ x }}", "", "", "", "", "action ref is missing"},
		{"${{ x }}", "", "", "", "", "action ref is missing"},
		{"o/r@${{ x }}", "github.com", "o/r", "", "${{ x }}", ""},
		{"o/r/${{ x }}@v", "github.com", "o/r", "${{ x }}", "v", ""},
		{"o/r@v/x", "github.com", "o/r", "", "v/x", ""},
		{"Docker://x", "", "", "", "", "action ref is missing"},
		{"DOCKER://x", "", "", "", "", "action ref is missing"},
		{"owner/repo/.github/workflows/ci.yml@v1", "github.com", "owner/repo", ".github/workflows/ci.yml", "v1", ""},
		{"o/r/.@v", "github.com", "o/r", ".", "v", ""},
		{"o:p/r@v", "github.com", "o:p/r", "", "v", ""},
		{"o/r\t@v", "github.com", "o/r\t", "", "v", ""},
		{".git/r@v", "github.com", ".git/r", "", "v", ""},
		{"$$/r@v", "github.com", "$$/r", "", "v", ""},
		{"o/r/p/@v", "", "", "", "", "invalid action directory \"p/\""},
		{"o/r//p@v", "", "", "", "", "invalid action directory \"/p\""},
		{"o/r/../p@v", "", "", "", "", "invalid action directory \"../p\""},
		{"o/r/p/..@v", "", "", "", "", "invalid action directory \"p/..\""},
		{"o/r/./p@v", "", "", "", "", "invalid action directory \"./p\""},
		{"o/r/\\p@v", "", "", "", "", "invalid action directory \"\\\\p\""},
		{"o/r//@v", "", "", "", "", "invalid action directory \"/\""},
		{"O/R/p@V", "github.com", "O/R", "p", "V", ""},
		{".", "", "", "", "", "action ref is missing"},
		{"./.", "", "", "", "", "action ref is missing"},
		{"$", "", "", "", "", "action ref is missing"},
		{"$/.@v", "", "", "", "", "action needs an owner and repository"},
		{"./o/r@v", "", "", "", "", "action needs an owner and repository"},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			ref, err := actions.ParseReference("GitHub.com", tc.spec)
			message := ""
			if err != nil {
				message = err.Error()
			}
			if message != tc.err {
				t.Fatalf("ParseReference(%q) error = %q, want %q", tc.spec, message, tc.err)
			}
			if ref.Host() != tc.host || ref.Repository() != tc.repository || ref.Directory() != tc.directory || ref.Ref() != tc.ref {
				t.Fatalf("ParseReference(%q) = %q %q %q %q, want %q %q %q %q", tc.spec, ref.Host(), ref.Repository(), ref.Directory(), ref.Ref(), tc.host, tc.repository, tc.directory, tc.ref)
			}
		})
	}
}
