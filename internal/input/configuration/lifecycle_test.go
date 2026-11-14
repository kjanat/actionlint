package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

func TestLifecycleMergeByID(t *testing.T) {
	t.Parallel()
	policy := func(id, description string) any {
		return map[string]any{"id": id, "description": description, "match": map[string]any{"subjects": []any{"runners"}, "events": []any{"support-ends"}}, "notifications": []any{map[string]any{"starts": map[string]any{"before": "90d"}, "severity": "warning"}}}
	}
	doc := func(p ...any) any {
		return map[string]any{"lint": map[string]any{"lifecycle": map[string]any{"policies": p}}}
	}
	merged := MergeDocuments(doc(policy("a", "original"), policy("b", "retained")), doc(policy("a", "replacement"), policy("c", "added")))
	var cfg Config
	if err := binding.Decode(merged, &cfg); err != nil {
		t.Fatal(err)
	}
	got, err := cfg.LifecyclePolicies()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Description() != "replacement" || got[1].ID() != "b" || got[2].ID() != "c" {
		t.Fatalf("%+v", got)
	}
	if _, err := (LifecycleConfig{Policies: []LifecyclePolicy{{ID: "x"}, {ID: "x"}}}).Compile(""); err == nil {
		t.Fatal("invalid policies accepted")
	}
}

func TestLifecycleInheritedOriginsAndOverrides(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	child := filepath.Join(dir, "child.yml")
	makePolicy := func(id string) string {
		return fmt.Sprintf("  - id: %s\n    description: description\n    match: {subjects: [runners], events: [support-ends]}\n    notifications: [{starts: {before: 90d}, severity: warning}]\n", id)
	}
	if err := os.WriteFile(base, []byte("lint:\n lifecycle:\n  policies:\n"+makePolicy("a")+makePolicy("b")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte("extends: [base.yml]\nlint:\n lifecycle:\n  policies:\n"+makePolicy("a")+makePolicy("c")), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := (RuleSet{}).Read(child, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loaded.Config.LifecyclePolicies()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("policies: %+v", got)
	}
	for i, want := range []string{child, base, child} {
		if got[i].Origin() != want {
			t.Fatalf("policy %s origin %q want %q", got[i].ID(), got[i].Origin(), want)
		}
	}
}
