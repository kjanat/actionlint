package cli_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestRulesGroupedTextAndStructuredJSON(t *testing.T) {
	t.Parallel()
	metadata := testRunCommand("", "rules", "--no-config", "--json")
	want, err := checking.RulesWithPlugins(t.Context(), plugininstall.Sources{})
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(want, func(a, b rule.Descriptor) int { return strings.Compare(a.Name, b.Name) })
	var rules []rule.Descriptor
	if err := json.Unmarshal([]byte(metadata.Stdout), &rules); err != nil || metadata.Status != 0 || metadata.Stderr != "" {
		t.Fatalf("rules JSON contract changed: %+v, %v", metadata, err)
	}
	for _, descriptors := range [][]rule.Descriptor{rules, want} {
		for i := range descriptors {
			if len(descriptors[i].OptionsSchema) == 0 {
				continue
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, descriptors[i].OptionsSchema); err != nil {
				t.Fatalf("invalid options schema for %s: %v", descriptors[i].Name, err)
			}
			descriptors[i].OptionsSchema = compact.Bytes()
		}
	}
	if !reflect.DeepEqual(rules, want) {
		if len(rules) != len(want) {
			t.Fatalf("rules JSON count=%d catalog count=%d", len(rules), len(want))
		}
		for i := range rules {
			if !reflect.DeepEqual(rules[i], want[i]) {
				t.Fatalf("rule JSON differs for %s: actual=%+v expected=%+v", rules[i].Name, rules[i], want[i])
			}
		}
		t.Fatal("rules JSON differs from catalog")
	}
	byName := map[string]rule.Descriptor{}
	for i, rule := range rules {
		if i > 0 && rules[i-1].Name >= rule.Name {
			t.Fatal("JSON rules are no longer alphabetical")
		}
		byName[rule.Name] = rule
	}
	if metadata, ok := byName["missing-action-metadata"]; !ok || metadata.Category != "conventions" {
		t.Fatalf("missing metadata rule: %+v", metadata)
	}
	got := testRunCommand("", "rules", "--no-config")
	if got.Status != 0 || got.Stderr != "" {
		t.Fatal(got)
	}
	categoryByHeading := map[string]string{
		"Correctness:": "correctness", "Security:": "security", "Suspicious:": "suspicious", "Conventions:": "conventions",
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
	wantHeadings := []string{"Correctness:", "Suspicious:", "Security:", "Conventions:"}
	if !slices.Equal(headings, wantHeadings) || len(byName) != 0 {
		t.Fatalf("headings=%v, missing rules=%v", headings, byName)
	}
}

func TestRulesIncludesSelectedPluginMetadata(t *testing.T) {
	t.Parallel()
	path := "internal/plugins/execution/component/testdata/plugins/no-sudo.wasm"
	got := testRunCommand("", "rules", "no-sudo", "--plugin", path, "--json")
	var descriptor rule.Descriptor
	if err := json.Unmarshal([]byte(got.Stdout), &descriptor); err != nil || got.Status != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if descriptor.Provider != "example" || descriptor.Category != "suspicious" || descriptor.Help == "" {
		t.Fatal(descriptor)
	}
	got = testRunCommand("", "rules", "--plugin", path, "--plugin", path)
	if got.Status == 0 || !strings.Contains(got.Stderr, "duplicate rule name") {
		t.Fatal(got)
	}
}
