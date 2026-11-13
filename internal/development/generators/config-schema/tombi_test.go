package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	configschema "actionlint.kjanat.dev/v2/internal/input/configuration/schema"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestTombiRuleOptions(t *testing.T) {
	t.Parallel()
	tombi, err := exec.LookPath("tombi")
	if err != nil {
		t.Skip("tombi is not installed")
	}
	generated := generatedSchema(t)
	schema, err := configschema.Document(func(name string) ([]byte, error) {
		if name == configschema.RootFile {
			return generated, nil
		}
		return os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(name)))
	}, []manifests.Manifest{{Rule: rule.Descriptor{
		Name: "custom-check", Category: "conventions", Description: "Check a numeric limit",
		OptionsSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(filename, schema, 0o600); err != nil {
		t.Fatal(err)
	}
	schemaURL := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(filename), "/")}).String()
	for _, tc := range []struct {
		name, config, diagnostic string
	}{
		{"valid options", "[lint.rules.conventions.job-timeout]\nlevel = 'error'\noptions = { max-minutes = 30 }\n", ""},
		{"unknown option", "[lint.rules.conventions.job-timeout]\nlevel = 'error'\noptions = { maximum = 30 }\n", "maximum"},
		{"invalid bound", "[lint.rules.conventions.job-timeout]\nlevel = 'error'\noptions = { max-minutes = -1 }\n", "the value must be > 0"},
		{"deprecated rule", "[lint.rules.conventions]\nrequire-job-timeout = 'error'\n", "Use job-timeout instead."},
		{"valid plugin options", "[lint.rules.conventions.custom-check]\nlevel = 'error'\noptions = { limit = 2 }\n", ""},
		{"invalid plugin options", "[lint.rules.conventions.custom-check]\nlevel = 'error'\noptions = { limit = 0 }\n", "the value must be ≥ 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, tombi, "lint", "--offline", "--error-on-warnings", "--stdin-filename=actionlint.toml", "-")
			command.Stdin = strings.NewReader("#:schema " + schemaURL + "\n" + tc.config)
			output, err := command.CombinedOutput()
			if tc.diagnostic == "" {
				if err != nil {
					t.Fatalf("valid options rejected: %v\n%s", err, output)
				}
			} else if err == nil || !strings.Contains(string(output), tc.diagnostic) {
				t.Fatalf("expected diagnostic containing %q: %v\n%s", tc.diagnostic, err, output)
			}
		})
	}
}
