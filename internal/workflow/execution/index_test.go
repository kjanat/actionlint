package execution_test

import (
	"fmt"
	"strconv"
	"testing"

	execution "actionlint.kjanat.dev/v2/internal/workflow/execution"
)

func TestExecutableBitCaseFoldedPaths(t *testing.T) {
	for _, tc := range []struct {
		name, runnerPath, checkout, want string
		modes                            map[string]string
	}{
		{"file", "./BAD.SH", "", "bad.sh", map[string]string{"bad.sh": "100644"}},
		{"directory", "SCRIPTS/BAD.SH", "", "scripts/bad.sh", map[string]string{"scripts/bad.sh": "100644"}},
		{"checkout", "SOURCE/SCRIPTS/BAD.SH", "source", "source/scripts/bad.sh", map[string]string{"scripts/bad.sh": "100644"}},
		{"parent traversal", "SCRIPTS/../BAD.SH", "", "bad.sh", map[string]string{"scripts/file": "100644", "bad.sh": "100644"}},
		{"missing parent", "MISSING/../BAD.SH", "", "", map[string]string{"bad.sh": "100644"}},
		{"file parent", "GOOD.SH/../BAD.SH", "", "", map[string]string{"good.sh": "100755", "bad.sh": "100644"}},
		{"symlink parent", "LINK/../BAD.SH", "", "", map[string]string{"link": "120000", "bad.sh": "100644"}},
		{"unmerged parent", "SCRIPTS/BAD.SH", "", "", map[string]string{"scripts": "unmerged", "scripts/bad.sh": "100644"}},
		{"ambiguous file", "bad.sh", "", "", map[string]string{"bad.sh": "100644", "BAD.SH": "100755"}},
		{"ambiguous directory", "scripts/bad.sh", "", "", map[string]string{"scripts/bad.sh": "100644", "SCRIPTS/good.sh": "100755"}},
		{"ambiguous symlink", "scripts/bad.sh", "", "", map[string]string{"scripts/bad.sh": "100644", "SCRIPTS": "120000"}},
		{"unsupported unicode", "É/BAD.SH", "", "", map[string]string{"é/bad.sh": "100644"}},
		{"Unicode folds to ASCII", "K/BAD.SH", "", "", map[string]string{"K/bad.sh": "100644"}},
		{"Unicode collides with ASCII", "S/BAD.SH", "", "", map[string]string{"s/bad.sh": "100644", "ſ/other.sh": "100644"}},
		{"unknown Unicode output", "scripts/é", "", "scripts/é", map[string]string{"scripts/file": "100644"}},
		{"nested checkout ancestor", "PARENT/SOURCE/../SOURCE/BAD.SH", "parent/source", "parent/source/bad.sh", map[string]string{"bad.sh": "100644"}},
		{"Unicode checkout", "K/BAD.SH", "K", "", map[string]string{"bad.sh": "100644"}},
		{"empty index preserves exact checkout", "source/new", "source", "source/new", nil},
		{"empty index cannot fold checkout", "SOURCE/new", "source", "", nil},
		{"outside checkout", "../BAD.SH", "", "", map[string]string{"bad.sh": "100644"}},
		{"new output", "SCRIPTS/OUTPUT", "", "scripts/OUTPUT", map[string]string{"scripts/file": "100644"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := &execution.IndexSnapshot{Modes: tc.modes}
			got, known := snapshot.CaseFoldedPath(tc.runnerPath, tc.checkout)
			if known != (tc.want != "") || got != tc.want {
				t.Fatalf("caseFoldedPath(%q) = %q, %v; want %q", tc.runnerPath, got, known, tc.want)
			}
		})
	}
}

func TestExecutableBitCaseFoldedCheckoutReuse(t *testing.T) {
	snapshot := &execution.IndexSnapshot{Modes: map[string]string{"scripts/bad.sh": "100644"}}
	for _, tc := range []struct{ runnerPath, checkout, want string }{
		{"SCRIPTS/BAD.SH", "", "scripts/bad.sh"},
		{"SOURCE/SCRIPTS/BAD.SH", "source", "source/scripts/bad.sh"},
		{"PARENT/SOURCE/SCRIPTS/BAD.SH", "parent/source", "parent/source/scripts/bad.sh"},
	} {
		if got, known := snapshot.CaseFoldedPath(tc.runnerPath, tc.checkout); !known || got != tc.want {
			t.Errorf("shared snapshot lookup under %q = %q, %v; want %q", tc.checkout, got, known, tc.want)
		}
	}
}

func BenchmarkExecutableBitCaseFoldedPath(b *testing.B) {
	for _, size := range []int{100, 100000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			modes := make(map[string]string, size)
			for i := range size {
				modes[fmt.Sprintf("packages/package%d/scripts/file.sh", i)] = "100644"
			}
			snapshot := &execution.IndexSnapshot{Modes: modes}
			const input = "SOURCE/PACKAGES/PACKAGE0/SCRIPTS/FILE.SH"
			if _, known := snapshot.CaseFoldedPath(input, "source"); !known {
				b.Fatal("benchmark path must resolve")
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, known := snapshot.CaseFoldedPath(input, "source"); !known {
					b.Fatal("benchmark path must resolve")
				}
			}
		})
	}
}

func BenchmarkExecutableBitPathIndexConstruction(b *testing.B) {
	modes := make(map[string]string, 100000)
	for i := range 100000 {
		modes[fmt.Sprintf("packages/package%d/scripts/file.sh", i)] = "100644"
	}
	b.ReportAllocs()
	for b.Loop() {
		snapshot := &execution.IndexSnapshot{Modes: modes}
		if !snapshot.DirectoryExists("packages/package0/scripts") {
			b.Fatal("benchmark directory must exist")
		}
	}
}

func TestExecutableBitIndexTraversal(t *testing.T) {
	snapshot := &execution.IndexSnapshot{Modes: map[string]string{"link": "120000", "submodule": "160000", "bad.sh": "100644", "scripts/bad.sh": "100644"}}
	for _, tc := range []struct {
		path, checkout string
		want           bool
	}{
		{"./bad.sh", "", true},
		{"scripts/../bad.sh", "", true},
		{"missing/../bad.sh", "", false},
		{"scripts/", "", true},
		{"missing/", "", false},
		{"bad.sh/", "", false},
		{"missing/../source/bad.sh", "source", false},
		{"link/../bad.sh", "", false},
		{"submodule/../bad.sh", "", false},
		{"source/link/../bad.sh", "source", false},
		{".source/link/../bad.sh", ".source", false},
		{"scripts/bad.sh/../bad.sh", "", false},
		{"../bad.sh", "", false},
	} {
		if got := snapshot.OrdinaryTraversal(tc.path, tc.checkout); got != tc.want {
			t.Errorf("traversal %q under %q: got %v", tc.path, tc.checkout, got)
		}
	}
}
