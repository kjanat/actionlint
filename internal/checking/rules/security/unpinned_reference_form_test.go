package security

import "testing"

func TestActionUsesRepoRefByForm(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec string
		ref  string
		ok   bool
	}{
		{"o/r@v", "v", true},
		{"o/r/p@v", "v", true},
		{"o/r@v@w", "v@w", true},
		{"o/r@v ${{ x }}", "v ${{ x }}", true},
		{"builtin:o/r@v", "v", true},
		{"self:o/r@v", "v", true},
		{"", "", false},
		{"./a@v", "", false},
		{"$/a@v", "", false},
		{"docker://x@sha256:ab", "", false},
		{"o/r", "", false},
		{"o/r@", "", false},
		{"o/@v", "", false},
		{"https://h/o/r@v", "", false},
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
