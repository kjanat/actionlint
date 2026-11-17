package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configschema"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

type matrixCase struct {
	Name         string `json:"name"`
	Extension    string `json:"extension"`
	Model        bool   `json:"model"`
	Schema       bool   `json:"schema"`
	Tombi        bool   `json:"tombi"`
	Specialized  bool   `json:"specialized"`
	ResolveRules bool   `json:"resolve_rules"`
	Reason       string `json:"reason"`
}

func TestConfigurationConsumerMatrix(t *testing.T) {
	t.Parallel()
	fixtureDir, err := filepath.Abs("testdata/tombi")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(fixtureDir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []matrixCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	generated := generatedSchema(t)
	bundled, err := toolmanifest.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	var fileManifest toolmanifest.Manifest
	for _, m := range bundled {
		if m.Rule.Name == "shellcheck" {
			fileManifest = m
			break
		}
	}
	fileManifest.ID = "example.test/providers/file-tool"
	fileManifest.Rule.Name = "file-tool"
	fileManifest.ConfigSchema = json.RawMessage(`{"$id":"settings.json","$defs":{"limit":{"type":"integer","minimum":1}},"type":"object","properties":{"limit":{"$ref":"#/$defs/limit"}},"additionalProperties":false}`)
	fileManifest.ConfigShorthands = map[string]toolsettings.Shorthand{"limit": {Path: []string{"limit"}, Precedence: "shorthand"}}
	fileManifest.Process.ConfigSchema = json.RawMessage(`{"$id":"settings.json","$defs":{"limit":{"type":"integer","maximum":10}},"type":"object","properties":{"limit":{"$ref":"#/$defs/limit"}},"additionalProperties":false}`)
	encoded, err := json.Marshal(fileManifest)
	if err != nil {
		t.Fatal(err)
	}
	fileManifest, err = toolmanifest.ParsePluginManifest(encoded)
	if err != nil {
		t.Fatal(err)
	}
	local := toolmanifest.Manifest{Rule: rule.Descriptor{Name: "local-tool", Category: "conventions", Description: "Local tool", OptionsSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`)}, ConfigSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`)}
	readSchema := func(name string) ([]byte, error) {
		if name == configschema.RootFile {
			return generated, nil
		}
		return os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(name)))
	}
	type consumer struct {
		validate func([]byte) error
		url      string
	}
	makeConsumer := func(ms []toolmanifest.Manifest) consumer {
		providers := make([]configschema.ToolProvider, 0, len(ms))
		for _, m := range ms {
			providers = append(providers, m.ConfigurationSchemaProvider())
		}
		b, err := configschema.Document(readSchema, providers)
		if err != nil {
			t.Fatal(err)
		}
		v, err := toolsettings.Compile(b)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "schema.json")
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		u := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}).String()
		return consumer{validate: v.Validate, url: u}
	}
	standard := makeConsumer(nil)
	specialized := makeConsumer([]toolmanifest.Manifest{local, fileManifest})
	tombi, tombiErr := exec.LookPath("tombi")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			ext := tc.Extension
			if ext == "" {
				ext = ".toml"
			}
			b, err := os.ReadFile(filepath.Join(fixtureDir, tc.Name+ext))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := document.ParseConfigDocument("actionlint"+ext, b)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			rules := checking.ConfigurationRules()
			result, modelErr := rules.ResolveValue(doc.Value(), nil, "example", "local-tool", "file-tool")
			if modelErr == nil && tc.ResolveRules {
				_, modelErr = rules.ResolveRules(configuration.Presets{}, result.Config)
			}
			if (modelErr == nil) != tc.Model {
				t.Errorf("model acceptance: want %t, error %v (%s)", tc.Model, modelErr, tc.Reason)
			}
			j, err := json.Marshal(doc.Value())
			if err != nil {
				t.Fatal(err)
			}
			c := standard
			if tc.Specialized {
				c = specialized
			}
			schemaErr := c.validate(j)
			if (schemaErr == nil) != tc.Schema {
				t.Errorf("schema acceptance: want %t, error %v (%s)", tc.Schema, schemaErr, tc.Reason)
			}
			if ext != ".toml" {
				return
			}
			t.Run("tombi", func(t *testing.T) {
				t.Parallel()
				if tombiErr != nil {
					t.Skip("tombi is not installed")
				}
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				diagnosticsPath := filepath.Join(t.TempDir(), "diagnostics.json")
				cmd := exec.CommandContext(ctx, tombi, "lint", "--offline", "--error-on-warnings", "--diagnostics-format=json", "--diagnostics-file="+diagnosticsPath, "--stdin-filename=actionlint.toml", "-")
				cmd.Stdin = strings.NewReader("#:schema " + c.url + "\n" + string(b))
				out, err := cmd.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatalf("tombi did not complete: %v", ctx.Err())
				}
				diagnosticsJSON, readErr := os.ReadFile(diagnosticsPath)
				if readErr != nil {
					t.Fatalf("read tombi diagnostics: %v\n%s", readErr, out)
				}
				var diagnostics []struct {
					Level string `json:"level"`
					Code  string `json:"code"`
				}
				if decodeErr := json.Unmarshal(diagnosticsJSON, &diagnostics); decodeErr != nil {
					t.Fatalf("decode tombi diagnostics: %v\n%s", decodeErr, diagnosticsJSON)
				}
				for _, diagnostic := range diagnostics {
					if (diagnostic.Level != "error" && diagnostic.Level != "warning") || diagnostic.Code == "" {
						t.Fatalf("unexpected tombi diagnostic: %+v", diagnostic)
					}
				}
				if err != nil {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || len(diagnostics) == 0 {
						t.Fatalf("tombi failed without a validation diagnostic: %v\n%s\n%s", err, out, diagnosticsJSON)
					}
				}
				if err == nil && len(diagnostics) != 0 {
					t.Fatalf("tombi exited successfully despite --error-on-warnings: %s", diagnosticsJSON)
				}
				if (err == nil) != tc.Tombi {
					t.Errorf("tombi acceptance: want %t, error %v\n%s\n%s\n%s", tc.Tombi, err, out, diagnosticsJSON, tc.Reason)
				}
			})
		})
	}
}
