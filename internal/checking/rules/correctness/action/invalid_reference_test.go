package action

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// TestInvalidActionReferenceByForm pins which uses spellings InvalidActionReference rejects.
func TestInvalidActionReferenceByForm(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec    string
		invalid bool
	}{
		{"", true},
		{" ", true},
		{"./a", false},
		{"./", false},
		{"./a@v", false},
		{".//a", false},
		{"$/a", false},
		{"$/a@v", true},
		{"$/", false},
		{"$//a", false},
		{"docker://x", false},
		{"docker://", false},
		{"docker://x:1", false},
		{"docker://x@sha256:ab", false},
		{"o/r@v", false},
		{"o/r/p@v", false},
		{"o/r/p/q@v", false},
		{"o/r", true},
		{"o/@v", true},
		{"/r@v", true},
		{"o//p@v", true},
		{"o/r@", true},
		{"@v", true},
		{"o@v", true},
		{"builtin:x", true},
		{"builtin:", true},
		{"builtin:o/r@v", false},
		{"self:o/r@v", false},
		{"self:o/r", true},
		{"https://h/o/r@v", true},
		{"https://h/o/r/p", true},
		{"http://h:3000/o/r@v", true},
		{"o/r@v ${{ x }}", false},
		{" o/r@v", false},
		{"o/r@v ", false},
		{"o/r@v@w", false},
		{"../a", true},
		{"o/../p@v", false},
		{"docker://x@sha256:abc", false},
		{"docker://x:latest", false},
		{"docker://x:", true},
		{"docker://:t", false},
		{"docker://x:t:u", false},
		{"docker://x/y:1.2@sha256:ab", false},
		{"docker:// x", true},
		{".@v", true},
		{"$@v", true},
		{"$x/r@v", false},
		{".x/r@v", false},
		{"./a ${{ x }}", false},
		{"docker://${{ x }}", true},
		{"${{ x }}", true},
		{"o/r@${{ x }}", false},
		{"Docker://x", true},
		{"DOCKER://x", true},
		{"owner/repo/.github/workflows/ci.yml@v1", false},
		{"o:p/r@v", false},
		{".git/r@v", false},
		{"$$/r@v", false},
		{"./o/r@v", false},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			if got := InvalidActionReference(tc.spec, workflowlang.Pos{Line: 1, Col: 1}, true); got != tc.invalid {
				t.Fatalf("InvalidActionReference(%q) = %v, want %v", tc.spec, got, tc.invalid)
			}
		})
	}
}
