package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func enforcedTestPolicy(id, description string) string {
	return fmt.Sprintf(`- id: %s
  description: %s
  match: {subjects: [runners], events: [support-ends]}
  notifications: [{starts: {before: 90d}, severity: warning}]
`, id, description)
}

func TestEnforcedLifecycleChains(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	child := filepath.Join(dir, "child.yml")
	second := filepath.Join(dir, "second.yml")
	for path, content := range map[string]string{
		base:   "lint:\n  lifecycle:\n    policies:\n" + indentEnforcedTest(enforcedTestPolicy("shared", "base")+enforcedTestPolicy("base-only", "inherited")),
		child:  "extends: [base.yml]\nlint:\n  lifecycle:\n    policies:\n" + indentEnforcedTest(enforcedTestPolicy("shared", "replacement")+enforcedTestPolicy("child-only", "child")),
		second: "lint:\n  lifecycle:\n    policies:\n" + indentEnforcedTest(enforcedTestPolicy("shared", "independent")),
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := ReadEnforcedLifecyclePolicies([]string{child, second}, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	policies := loaded.Policies()
	if len(policies) != 4 {
		t.Fatalf("got %d policies, want 4", len(policies))
	}
	for i, want := range []struct{ id, description, origin string }{
		{"shared", "replacement", child},
		{"base-only", "inherited", base},
		{"child-only", "child", child},
		{"shared", "independent", second},
	} {
		if got := policies[i]; got.ID() != want.id || got.Description() != want.description || got.Origin() != want.origin {
			t.Errorf("policy %d = %q %q %q, want %+v", i, got.ID(), got.Description(), got.Origin(), want)
		}
	}
	policies[0] = policies[1]
	if loaded.Policies()[0].ID() != "shared" {
		t.Fatal("Policies returned mutable internal slice")
	}
	files := loaded.Files()
	if len(files) != 3 {
		t.Fatalf("files = %v", files)
	}
	files[0] = "changed"
	if loaded.Files()[0] == "changed" {
		t.Fatal("Files returned mutable internal slice")
	}
}

func indentEnforcedTest(value string) string {
	return "      " + strings.ReplaceAll(strings.TrimSuffix(value, "\n"), "\n", "\n      ") + "\n"
}

func TestEnforcedLifecycleRejectsInvalidLayers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, base, child string }{
		{"empty chain", "{}", "extends: [base.yml]"},
		{"unknown root", "tools: {}", "extends: [base.yml]"},
		{"unknown lint", "lint: {rules: {}}", "extends: [base.yml]"},
		{"invalid schema", "$schema: 1", "extends: [base.yml]"},
		{"invalid inherited policy", "lint:\n  lifecycle:\n    policies:\n      - {id: invalid}\n", "extends: [base.yml]\nlint:\n  lifecycle:\n    policies:\n" + indentEnforcedTest(enforcedTestPolicy("valid", "valid"))},
		{"missing base", "", "extends: [missing.yml]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "base.yml"), []byte(tc.base), 0o600); err != nil {
				t.Fatal(err)
			}
			child := filepath.Join(dir, "child.yml")
			if err := os.WriteFile(child, []byte(tc.child), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadEnforcedLifecyclePolicies([]string{child}, os.ReadFile); err == nil {
				t.Fatal("invalid chain was accepted")
			}
		})
	}
}

func TestEnforcedLifecycleRemoteSourcesUseReader(t *testing.T) {
	t.Parallel()
	const base = "https://example.com/policy/base.yml"
	const child = "https://example.com/policy/child.yml"
	files := map[string]string{
		base:  "lint:\n  lifecycle:\n    policies:\n" + indentEnforcedTest(enforcedTestPolicy("remote", "remote")),
		child: "$schema: https://example.com/schema.json\nextends: [base.yml]\n",
	}
	loaded, err := ReadEnforcedLifecyclePolicies([]string{child}, func(path string) ([]byte, error) {
		content, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Policies(); len(got) != 1 || got[0].Origin() != base {
		t.Fatalf("remote policy origin: %v", got)
	}
}

func TestEnforcedLifecycleFormats(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, contents string }{
		{"policy.yml", "lint:\n  lifecycle:\n    policies:\n" + indentEnforcedTest(enforcedTestPolicy("format", "format"))},
		{"policy.json", `{"lint":{"lifecycle":{"policies":[{"id":"format","description":"format","match":{"subjects":["runners"],"events":["support-ends"]},"notifications":[{"starts":{"before":"90d"},"severity":"warning"}]}]}}}`},
		{"policy.toml", "[[lint.lifecycle.policies]]\nid = 'format'\ndescription = 'format'\nmatch = { subjects = ['runners'], events = ['support-ends'] }\nnotifications = [{ starts = { before = '90d' }, severity = 'warning' }]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := ReadEnforcedLifecyclePolicies([]string{path}, os.ReadFile)
			if err != nil {
				t.Fatal(err)
			}
			if got := loaded.Policies(); len(got) != 1 || got[0].ID() != "format" || got[0].Origin() != path {
				t.Fatalf("policies = %v", got)
			}
		})
	}
}
