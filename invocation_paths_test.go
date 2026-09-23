package actionlint

import "testing"

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
		{"ambiguous file", "bad.sh", "", "", map[string]string{"bad.sh": "100644", "BAD.SH": "100755"}},
		{"ambiguous directory", "scripts/bad.sh", "", "", map[string]string{"scripts/bad.sh": "100644", "SCRIPTS/good.sh": "100755"}},
		{"ambiguous symlink", "scripts/bad.sh", "", "", map[string]string{"scripts/bad.sh": "100644", "SCRIPTS": "120000"}},
		{"unsupported unicode", "É/BAD.SH", "", "", map[string]string{"é/bad.sh": "100644"}},
		{"outside checkout", "../BAD.SH", "", "", map[string]string{"bad.sh": "100644"}},
		{"new output", "SCRIPTS/OUTPUT", "", "scripts/OUTPUT", map[string]string{"scripts/file": "100644"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := &gitModeSnapshot{modes: tc.modes}
			got, known := snapshot.caseFoldedPath(tc.runnerPath, tc.checkout)
			if known != (tc.want != "") || got != tc.want {
				t.Fatalf("caseFoldedPath(%q) = %q, %v; want %q", tc.runnerPath, got, known, tc.want)
			}
		})
	}
}
