package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	configschema "actionlint.kjanat.dev/v2/internal/input/configuration/schema"
	configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"

	providerconfig "actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"
	"github.com/kjanat/go-yaml/v4"
)

func uncommentConfigExamples(document string) string {
	setting := regexp.MustCompile(`^\s*(?:[a-z][a-z-]*:|[".].*:|- )`)
	var example strings.Builder
	for line := range strings.SplitSeq(document, "\n") {
		if strings.HasPrefix(line, "# yaml-language-server:") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if strings.HasPrefix(line[indent:], "# ") {
			line = line[:indent] + line[indent+2:]
		}
		if setting.MatchString(line) {
			example.WriteString(line)
			example.WriteString("\n")
		}
	}
	return example.String()
}

func TestDefaultConfigFormats(t *testing.T) {
	t.Parallel()
	document, err := configschema.Document(configschema.Schema, nil)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := providerconfig.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range repoconfig.FileFormats() {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			content, err := checking.ConfigurationRules().DefaultDocument("2.0.0-SNAPSHOT-02d216d", format)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(content), "actionlint@02d216d/actionlint.schema.json") || strings.Contains(string(content), "{{SCHEMA_URL}}") || strings.Contains(string(content), "{{RULE_") {
				t.Fatalf("incorrect schema URL or unexpanded template:\n%s", content)
			}
			path := writeConfig(t, t.TempDir(), format.Filename(), string(content))
			if _, err := checking.ConfigurationRules().Load(path); err != nil {
				t.Fatal(err)
			}
			checkSchema := func(content []byte) {
				t.Helper()
				node, err := configtree.Decode(path, content)
				if err != nil {
					t.Fatal(err)
				}
				value := node.Value
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := schema.Validate(encoded); err != nil {
					t.Fatal(err)
				}
			}
			checkSchema(content)
			if format == repoconfig.FormatJSONC || format == repoconfig.FormatTOML {
				prefix := "// "
				if format == repoconfig.FormatTOML {
					prefix = "# "
				}
				_, examples, found := strings.Cut(string(content), prefix+"Rule defaults in this build:\n")
				if !found {
					t.Fatal("missing rule reference")
				}
				var reference strings.Builder
				for line := range strings.SplitSeq(strings.TrimSuffix(examples, "\n"), "\n") {
					reference.WriteString(strings.TrimPrefix(line, prefix))
					reference.WriteByte('\n')
				}
				checkSchema([]byte(reference.String()))
			}
		})
	}
}

func TestDefaultConfigUsesRuleDeclarations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, category, maturity, level, want string
	}{
		{"first-name", "security", "stable", "warn", "warn"},
		{"renamed-rule", "suspicious", "stable", "info", "info"},
		{"renamed-rule", "conventions", "nursery", "error", "off"},
	} {
		t.Run(tc.name+"/"+tc.category, func(t *testing.T) {
			t.Parallel()
			rules, err := repoconfig.NewRuleSet([]rule.Declaration{{
				Name: tc.name, Category: tc.category, Maturity: tc.maturity,
				Aliases:     []rule.Alias{{Name: "previous-name", Deprecated: true}},
				Recommended: true, DefaultLevel: tc.level,
			}})
			if err != nil {
				t.Fatal(err)
			}
			content, err := rules.DefaultDocument("2.0.0", repoconfig.FormatYAML)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(content), "previous-name") || strings.Contains(string(content), "{{RULE_") {
				t.Fatalf("generated reference contains an alias or an unexpanded template: %s", content)
			}
			type lintExample struct {
				Rules map[string]yaml.Node `yaml:"rules"`
			}
			var parsed struct {
				Lint      lintExample `yaml:"lint"`
				Overrides []struct {
					Lint lintExample `yaml:"lint"`
				} `yaml:"overrides"`
			}
			if err := yaml.Unmarshal([]byte(uncommentConfigExamples(string(content))), &parsed); err != nil {
				t.Fatal(err)
			}
			if len(parsed.Overrides) != 1 {
				t.Fatalf("generated %d overrides, want 1", len(parsed.Overrides))
			}
			for context, example := range map[string]lintExample{"defaults": parsed.Lint, "override": parsed.Overrides[0].Lint} {
				for category, node := range example.Rules {
					if node.Kind != yaml.MappingNode {
						continue
					}
					if category != tc.category {
						t.Errorf("%s uses category %q, want %q", context, category, tc.category)
					}
				}
				group, found := example.Rules[tc.category]
				if !found {
					t.Fatalf("%s omits category %q", context, tc.category)
				}
				var settings map[string]string
				if err := group.Decode(&settings); err != nil {
					t.Fatal(err)
				}
				want := tc.want
				if context == "override" {
					want = "off"
				}
				if len(settings) != 1 || settings[tc.name] != want {
					t.Errorf("%s settings = %v, want %s: %s", context, settings, tc.name, want)
				}
			}
		})
	}
}

func TestDefaultConfigReferenceExamples(t *testing.T) {
	t.Parallel()
	document, err := configschema.Document(configschema.Schema, nil)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := providerconfig.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	generatedBytes, err := checking.ConfigurationRules().DefaultDocument("2.0.0", repoconfig.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(generatedBytes)
	for name, content := range map[string]string{"defaults": generated, "examples": uncommentConfigExamples(generated)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var value any
			if err := yaml.Unmarshal([]byte(content), &value); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(encoded); err != nil {
				t.Errorf("generated config fails its schema: %v", err)
			}
			root := t.TempDir()
			for _, directory := range []string{".github", "config"} {
				if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			writeConfig(t, filepath.Join(root, "config"), "actionlint-base.yml", "{}\n")
			path := writeConfig(t, filepath.Join(root, ".github"), "actionlint.yaml", content)
			if _, err := checking.ConfigurationRules().Load(path); err != nil {
				t.Errorf("generated config fails the configuration loader: %v", err)
			}
		})
	}
}
