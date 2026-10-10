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

func TestMatchNormalizesPatterns(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selection Selection
		want      bool
	}{
		{"include", Selection{Includes: []string{"./local/*"}}, true},
		{"exclude", Selection{Excludes: []string{"./local/*"}}, false},
		{"negative include", Selection{Includes: []string{"**", "!./local/*"}}, false},
		{"negative exclusion", Selection{Excludes: []string{"!./local/*"}}, false},
		{"repeated dot prefix", Selection{Includes: []string{"././local/*"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, file := range []string{"local/file.yml", "./local/file.yml"} {
				if got := tc.selection.Match(file); got != tc.want {
					t.Fatalf("Match(%q)=%v, want %v", file, got, tc.want)
				}
			}
		})
	}
	for _, pattern := range []string{"**", "**/*", "./**", "./**/*"} {
		for _, file := range []string{"action.yml", "local/action.yml", ".github/workflows/ci.yml"} {
			if !(Selection{Includes: []string{pattern}}).Match(file) {
				t.Errorf("catch-all %q missed %q", pattern, file)
			}
		}
	}
}
