package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/repository"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

func TestLifecycleMergeByID(t *testing.T) {
	t.Parallel()
	policy := func(id, description string) any {
		return map[string]any{"id": id, "description": description, "match": map[string]any{"subjects": []any{"runners"}, "events": []any{"support-ends"}}, "notifications": []any{map[string]any{"starts": map[string]any{"before": "90d"}, "severity": "warning"}}}
	}
	doc := func(p ...any) any {
		return map[string]any{"lint": map[string]any{"lifecycle": map[string]any{"policies": p}}}
	}
	merged := configtree.Overlay(configtree.Capture(doc(policy("a", "original"), policy("b", "retained")), nil), configtree.Capture(doc(policy("a", "replacement"), policy("c", "added")), nil), MergePlan()).Value()
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
	if err := os.WriteFile(base, []byte("lint:\n lifecycle:\n  policies:\n"+makePolicy("a")+makePolicy("b")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte("extends: [base.yml]\nlint:\n lifecycle:\n  policies:\n"+makePolicy("a")+makePolicy("c")), 0o600); err != nil {
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

func TestLifecycleOverridesRetainOrigins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	child := filepath.Join(dir, "child.yml")
	input := "lint:\n  lifecycle:\n    policies:\n      - id: one\n        description: one\n        match: {subjects: [runners], events: [support-ends]}\n        notifications: [{starts: {before: 90d}, severity: warning}]\n      - id: two\n        description: two\n        match: {subjects: [runners], events: [support-ends]}\n        notifications: [{starts: {before: 90d}, severity: warning}]\n"
	override := "extends: [base.yml]\noverrides:\n  - includes: [release.yml]\n    lint:\n      lifecycle:\n        policies:\n          - id: two\n            description: replaced\n            match: {subjects: [runners], events: [support-ends]}\n            notifications: [{starts: {before: 30d}, severity: error}]\n"
	if err := os.WriteFile(base, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte(override), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := RuleSet{}
	loaded, err := rules.Read(child, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := rules.ForFile(loaded.Config, "release.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	policies, err := cfg.LifecyclePolicies()
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 2 || policies[0].Origin() != base || policies[1].Origin() != child || policies[1].Description() != "replaced" {
		t.Fatalf("policies=%+v", policies)
	}
}

func TestRepositoryNullThenPartialRetainsUnknownFacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	child := filepath.Join(dir, "child.yml")
	if err := os.WriteFile(base, []byte("repository: null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte("extends: [base.yml]\nrepository: {service: github.com}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := (RuleSet{}).Read(child, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := services.Parse("ghes", "")
	if err != nil {
		t.Fatal(err)
	}
	labels, err := repository.NewNames("custom")
	if err != nil {
		t.Fatal(err)
	}
	host := repository.Snapshot{Service: svc, RunnerLabels: labels}
	got := loaded.Config.Repository.Apply(host)
	if got.Service.Kind() != services.GitHubCom || got.RunnerLabels.Reason() != availability.NotFetchedReason {
		t.Fatalf("repository facts after reset: %+v", got)
	}
	origin := loaded.Inputs["/repository"]
	if origin.File != child || origin.Reset == nil || origin.Reset.File != base {
		t.Fatalf("reset origin=%+v", origin)
	}
}
