package main

import (
	"strings"
	"testing"
)

func TestGlobMatchAnchorsDirectoriesAtPathElements(t *testing.T) {
	for _, tc := range []struct {
		pattern, file string
		want          bool
	}{
		{"**/config/*.go", "/m/config/a.go", true},
		{"**/config/*.go", "/m/internal/config/a.go", true},
		{"**/config/*.go", "/m/internal/repoconfig/a.go", false},
		{"**/config/*.go", "/m/config/sub/a.go", false},
		{"**/config/*_test.go", "/m/config/a.go", false},
		{"**/actionlint.go", "/m/actionlint.go", true},
		{"$all", "/m/x/y.go", true},
		{"$test", "/m/x/y_test.go", true},
		{"$test", "/m/x/y.go", false},
	} {
		if got := globMatch(tc.pattern, tc.file); got != tc.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tc.pattern, tc.file, got, tc.want)
		}
	}
}

// depguard checks only the greatest entry sorted at or before the import, so
// a prefix entry can hide behind an unrelated exact entry. The model must
// reproduce that, or verifyRules would approve rules depguard rejects.
func TestInPrefixListChecksOnlyTheNearestEntry(t *testing.T) {
	list := []string{"a/b", "a/b/c$"}
	if ok, _ := inPrefixList("a/b/x", list); ok {
		t.Error("a/b/x matched although depguard compares it with a/b/c$ only")
	}
	if ok, _ := inPrefixList("a/b/c", list); !ok {
		t.Error("exact entry a/b/c$ did not match a/b/c")
	}
	if ok, _ := inPrefixList("a/bc", []string{"a/b$"}); ok {
		t.Error("exact entry a/b$ matched a/bc")
	}
	if ok, _ := inPrefixList("github.com/x", []string{"os"}); ok {
		t.Error("a standard prefix matched a path with a dot")
	}
}

func TestStrictRuleDeniesStandardPackageBelowGostdPrefix(t *testing.T) {
	c := compile(rule{strict: true, allow: []string{"$gostd"}, deny: []denial{{pkg: "os/exec"}}}, []string{"os", "fmt"})
	if c.allows("os/exec") {
		t.Error("os/exec allowed although denied")
	}
	if !c.allows("os") || !c.allows("fmt") {
		t.Error("standard packages outside the denial were rejected")
	}
}

func TestMatchPattern(t *testing.T) {
	for _, tc := range []struct {
		pattern, rel string
		want         bool
	}{
		{".", "", true},
		{".", "config", false},
		{"config", "config", true},
		{"config", "config/report", false},
		{"internal/semantics/...", "internal/semantics", true},
		{"internal/semantics/...", "internal/semantics/glob", true},
		{"internal/semantics/...", "internal/semanticsx", false},
	} {
		if got := matchPattern(tc.pattern, tc.rel); got != tc.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tc.pattern, tc.rel, got, tc.want)
		}
	}
}

func TestParseSpecRejectsForwardAllow(t *testing.T) {
	_, err := parseSpec([]byte(`
[[layer]]
name = "low"
doc = "low"
packages = ["a"]
allow = ["high"]

[[layer]]
name = "high"
doc = "high"
packages = ["b"]
`))
	if err == nil || !strings.Contains(err.Error(), "not a layer declared earlier") {
		t.Fatalf("got %v, want an error for an allow entry naming a later layer", err)
	}
}

func TestParseSpecRequiresReasonsForDeniedStandardPackages(t *testing.T) {
	_, err := parseSpec([]byte(`
[[layer]]
name = "low"
doc = "low"
packages = ["a"]
deny-std = ["os/exec"]
`))
	if err == nil || !strings.Contains(err.Error(), "std-reasons") {
		t.Fatalf("got %v, want an error for a denial without a reason", err)
	}
}

func TestSpliceReplacesOnlyTheGeneratedBlock(t *testing.T) {
	config := "before\n" + beginMarker + "\nold\n" + endMarker + "\nafter\n"
	got, err := splice([]byte(config), "new\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "before\n" + beginMarker + "\n\nnew\n\n" + endMarker + "\nafter\n"
	if string(got) != want {
		t.Errorf("splice:\n%s\nwant:\n%s", got, want)
	}
	if _, err := splice([]byte("no markers\n"), "new\n"); err == nil {
		t.Error("splice accepted a configuration without markers")
	}
}
