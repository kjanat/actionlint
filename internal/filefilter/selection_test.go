package filefilter

import "testing"

func TestValidPatternPortablePaths(t *testing.T) {
	for _, pattern := range []string{"/tmp/*", "!/tmp/*", "//server/share/*", `C:/tmp/*`, `C:\tmp\*`, `\tmp\*`, "../*", "a/../b", "", "!", "["} {
		if ValidPattern(pattern) {
			t.Errorf("accepted non-relative or invalid glob %q", pattern)
		}
	}
	for _, pattern := range []string{"**/*.yml", "!generated/**", ".github/workflows/*", "./local/*", "a/*/b"} {
		if !ValidPattern(pattern) {
			t.Errorf("rejected repository-relative glob %q", pattern)
		}
	}
}
