package workflownames

import "testing"

func TestPatternLineBreaks(t *testing.T) {
	for _, pattern := range []string{"Build\nCI", "Build\rCI", "Build\r\nCI", "!Build\nCI", "Build\\\nCI"} {
		if ValidPattern(pattern) {
			t.Errorf("accepted line break in %q", pattern)
		}
		names := Names{Values: map[string]bool{"Build": true}, Complete: true}
		if names.Missing(pattern) || names.ExcludesAll([]string{"Build", "!Build", pattern}) {
			t.Errorf("invalid pattern produced a name finding: %q", pattern)
		}
	}
}

func TestMissingPattern(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		missing       bool
	}{
		{"Build*", "Build CI", false},
		{"Build*", "Build", false},
		{"Build*", "build CI", true},
		{"Build*", "Build/CI", true},
		{"Build**", "Build/CI", false},
		{"Build/**/CI", "Build/CI", false},
		{"Build/**/CI", "Build/Linux/CI", false},
		{"**/CI", "CI", false},
		{"Builds?", "Build", false},
		{"Builds?", "Builds", false},
		{"Builds?", "Buildss", true},
		{"Builds+", "Buildss", false},
		{"Builds+", "Build", true},
		{"Build [CL]I", "Build CI", false},
		{"Build [0-9]+", "Build 123", false},
		{"Build [A-Z]+", "Build CI", false},
		{"Build [a-z]+", "Build ci", false},
		{"Build [0-9A-Za-z]+", "Build CI12x", false},
		{"Build [0-9]+", "Build x", true},
		{`Build C\+\+`, "Build C++", false},
		{`Build \*`, "Build *", false},
		{`Build \*`, "Build CI", true},
		{`Build \?`, "Build ?", false},
		{`Build \[CI]`, "Build [CI]", false},
		{`Build \]`, "Build ]", false},
		{`\[Build\]`, "[Build]", false},
		{`\[Build\]`, "Build", true},
		{`\!Build`, "!Build", false},
		{`\!Build`, "Build", true},
		{`Build\\CI`, `Build\CI`, false},
		{"Build (CI).{x}|^$!", "Build (CI).{x}|^$!", false},
		{"Build.1", "Buildx1", true},
		{"Build!", "Build!", false},
		{"!Missing*", "Build", false},
		{"Build [", "Build", false},
		{"Build [z-a]", "Build", false},
		{"Build [a-]", "Build", false},
		{"Build [-a]", "Build", false},
		{"Build [^x]", "Build", false},
		{"Build++", "Build", false},
		{"?Build", "Build", false},
		{"", "Build", false},
		{"Build", "Build CI", true},
		{"Build*", "Pre Build CI", true},
	} {
		t.Run(tc.pattern+"/"+tc.name, func(t *testing.T) {
			names := Names{Values: map[string]bool{tc.name: true}, Complete: true}
			if got := names.Missing(tc.pattern); got != tc.missing {
				t.Fatalf("Missing(%q) for %q = %v, want %v", tc.pattern, tc.name, got, tc.missing)
			}
		})
	}
	if (Names{Values: map[string]bool{}, Complete: false}).Missing("Missing") {
		t.Fatal("incomplete inventory reported a missing name")
	}
}

func TestCompilePatternInvalidRanges(t *testing.T) {
	for _, pattern := range []string{"[A-z]", "[0-z]", "[a-Z]", "[z-a]", "[a-]", "[-a]"} {
		t.Run(pattern, func(t *testing.T) {
			if _, ok := compilePattern(pattern); ok {
				t.Fatalf("accepted invalid character range %q", pattern)
			}
		})
	}
}

func TestExcludesAllPatterns(t *testing.T) {
	for _, tc := range []struct {
		patterns []string
		names    []string
		want     bool
	}{
		{[]string{"Build*", "!Build docs"}, []string{"Build docs"}, true},
		{[]string{"Build*", "!Build docs"}, []string{"Build docs", "Build CI"}, false},
		{[]string{"Build*", "!Build docs", "!Build CI"}, []string{"Build docs", "Build CI"}, true},
		{[]string{"Build*", "!Build docs", "Build docs"}, []string{"Build docs"}, false},
		{[]string{"!Build docs", "Build*"}, []string{"Build docs"}, false},
		{[]string{`\!Build*`, `!\!Build docs`}, []string{"!Build docs"}, true},
		{[]string{"Build**", "!Build/**"}, []string{"Build/docs"}, true},
		{[]string{"Build*", "!build docs"}, []string{"Build docs"}, false},
		{[]string{"Build*", "!Build docs", "Build ["}, []string{"Build docs"}, false},
		{[]string{"Build*", "!Build docs", ""}, []string{"Build docs"}, false},
		{[]string{"!Build docs"}, []string{"Build docs"}, false},
		{[]string{"Missing*", "!Build docs"}, []string{"Build docs"}, false},
		{nil, []string{"Build docs"}, false},
		{[]string{"Build*", "!Build docs"}, nil, false},
	} {
		names := Names{Complete: true, Values: map[string]bool{}}
		for _, name := range tc.names {
			names.Values[name] = true
		}
		if got := names.ExcludesAll(tc.patterns); got != tc.want {
			t.Errorf("%v against %v: got %v, want %v", tc.patterns, tc.names, got, tc.want)
		}
		names.Complete = false
		if names.ExcludesAll(tc.patterns) {
			t.Errorf("incomplete inventory reported exclusions: %v", tc.patterns)
		}
	}
}
