package cli

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	actionlint "actionlint.kjanat.dev/internal/engine"
)

func TestRulesGroupedTextAndStructuredJSON(t *testing.T) {
	metadata := testRunCommand("", "rules", "--json")
	var rules []actionlint.RuleInfo
	if err := json.Unmarshal([]byte(metadata.Stdout), &rules); err != nil || metadata.Status != 0 || metadata.Stderr != "" || !reflect.DeepEqual(rules, commandRules()) {
		t.Fatalf("rules JSON contract changed: %+v, %v", metadata, err)
	}
	byName := map[string]actionlint.RuleInfo{}
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
	want := []string{"Correctness:", "Security:", "Suspicious:", "Policy:", "External linters:"}
	if !slices.Equal(headings, want) || len(byName) != 0 {
		t.Fatalf("headings=%v, missing rules=%v", headings, byName)
	}
}
