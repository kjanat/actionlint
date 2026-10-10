package workflownames

import "testing"

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
