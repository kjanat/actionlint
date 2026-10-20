package cli_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/rulecatalog"

	"actionlint.kjanat.dev/v2/rule"
)

func TestRulesGroupedTextAndStructuredJSON(t *testing.T) {
	metadata := testRunCommand("", "rules", "--json")
	want := rulecatalog.Rules()
	slices.SortFunc(want, func(a, b rule.Descriptor) int { return strings.Compare(a.Name, b.Name) })
	var rules []rule.Descriptor
	if err := json.Unmarshal([]byte(metadata.Stdout), &rules); err != nil || metadata.Status != 0 || metadata.Stderr != "" || !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules JSON contract changed: %+v, %v", metadata, err)
	}
	byName := map[string]rule.Descriptor{}
	for i, rule := range rules {
		if i > 0 && rules[i-1].Name >= rule.Name {
			t.Fatal("JSON rules are no longer alphabetical")
		}
		byName[rule.Name] = rule
	}
	got := testRunCommand("", "rules")
	if got.Status != 0 || got.Stderr != "" {
		t.Fatal(got)
	}
	categoryByHeading := map[string]string{
		"Correctness:": "correctness", "Security:": "security", "Suspicious:": "suspicious",
		"Policy:": "policy", "External linters:": "external",
	}
	var headings []string
	category, previous := "", ""
	for line := range strings.SplitSeq(got.Stdout, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if next, ok := categoryByHeading[line]; ok {
			headings = append(headings, line)
			category, previous = next, ""
			continue
		}
		name := strings.Fields(line)[0]
		rule, ok := byName[name]
		description := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), name))
		if !ok || category == "" || rule.Category != category || name <= previous || description != rule.Description {
			t.Fatalf("incorrect rule grouping or repeated category: %q, category=%q, previous=%q", line, category, previous)
		}
		previous = name
		delete(byName, name)
	}
	wantHeadings := []string{"Correctness:", "Security:", "Suspicious:", "Policy:"}
	if !slices.Equal(headings, wantHeadings) || len(byName) != 0 {
		t.Fatalf("headings=%v, missing rules=%v", headings, byName)
	}
}

func TestRulesIncludesSelectedPluginMetadata(t *testing.T) {
	path := "internal/plugins/component/testdata/plugins/no-sudo.wasm"
	got := testRunCommand("", "rules", "no-sudo", "--plugin", path, "--json")
	var descriptor rule.Descriptor
	if err := json.Unmarshal([]byte(got.Stdout), &descriptor); err != nil || got.Status != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if descriptor.Provider != "example" || descriptor.Category != "portability" || descriptor.Help == "" {
		t.Fatal(descriptor)
	}
	got = testRunCommand("", "rules", "--plugin", path, "--plugin", path)
	if got.Status == 0 || !strings.Contains(got.Stderr, "duplicate rule name") {
		t.Fatal(got)
	}
}
