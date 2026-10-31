package filefilter

import (
	"testing"

	"github.com/kjanat/go-yaml/v4"
)

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
		name               string
		includes, excludes []string
		want               bool
	}{
		{"include", []string{"./local/*"}, nil, true},
		{"exclude", nil, []string{"./local/*"}, false},
		{"negative include", []string{"**", "!./local/*"}, nil, false},
		{"negative exclusion", nil, []string{"!./local/*"}, false},
		{"repeated dot prefix", []string{"././local/*"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selection, err := NewSelection(tc.includes, tc.excludes)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range []string{"local/file.yml", "./local/file.yml"} {
				if got := selection.Match(file); got != tc.want {
					t.Fatalf("Match(%q)=%v, want %v", file, got, tc.want)
				}
			}
		})
	}
	for _, pattern := range []string{"**", "**/*", "./**", "./**/*"} {
		for _, file := range []string{"action.yml", "local/action.yml", ".github/workflows/ci.yml"} {
			if !MatchPatterns([]string{pattern}, nil, file) {
				t.Errorf("catch-all %q missed %q", pattern, file)
			}
		}
	}
}

func TestNewSelectionOwnsValidatedPatterns(t *testing.T) {
	if _, err := NewSelection([]string{"../*"}, nil); err == nil {
		t.Fatal("accepted escaping include")
	}
	if _, err := NewSelection(nil, []string{"/tmp/*"}); err == nil {
		t.Fatal("accepted absolute exclusion")
	}
	includes := []string{"local/*"}
	selection, err := NewSelection(includes, nil)
	if err != nil {
		t.Fatal(err)
	}
	includes[0] = "other/*"
	if !selection.Match("local/file.yml") || selection.Match("other/file.yml") {
		t.Fatal("selection aliases caller storage")
	}
	if empty, err := NewSelection([]string{}, nil); err != nil || empty.IsZero() || empty.Match("file.yml") {
		t.Fatalf("explicit empty includes = %v, %v; want non-zero selection matching nothing", empty, err)
	}
}

func TestSelectionYAMLRoundTrip(t *testing.T) {
	for _, source := range []string{"includes: []\n", "excludes:\n    - local/skip.yml\nincludes:\n    - local/*\n"} {
		var selection Selection
		if err := yaml.Unmarshal([]byte(source), &selection); err != nil {
			t.Fatal(err)
		}
		encoded, err := yaml.Marshal(selection)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != source {
			t.Errorf("round trip of %q = %q", source, encoded)
		}
	}
	var selection Selection
	if err := yaml.Unmarshal([]byte("includes: [../*]\n"), &selection); err == nil {
		t.Fatal("accepted escaping include")
	}
}
