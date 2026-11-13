package schema

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	pluginmanifests "actionlint.kjanat.dev/v2/internal/plugins/manifests"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"
)

// moduleRoot reads the generated actionlint.schema.json and schemas/.
func moduleRoot(name string) ([]byte, error) { return fs.ReadFile(os.DirFS("../../../.."), name) }

// installed is a tool provider a plugin installs. Its settings accept only
// level: low or high.
func installed(configFiles bool) pluginmanifests.Manifest {
	manifest := pluginmanifests.Manifest{
		Rule:         rule.Descriptor{Name: "oci-tool", Description: "A tool from an OCI registry", Category: "conventions"},
		ConfigSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"level":{"enum":["low","high"]}},"additionalProperties":false}`),
		Process:      &pluginmanifests.ProcessSpec{},
	}
	if configFiles {
		manifest.Process.ConfigFiles = &pluginmanifests.ConfigFiles{Names: []string{"oci-tool.toml"}}
	}
	return manifest
}

func compile(t *testing.T, manifests ...pluginmanifests.Manifest) *configschema.Schema {
	t.Helper()
	document, err := Document(moduleRoot, manifests)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(document), `"$ref": "schemas/`) {
		t.Fatal("printed schema keeps a reference to a file beside actionlint.schema.json")
	}
	// Compile refuses external references, so this also proves the document
	// stands alone.
	schema, err := configschema.Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestDocumentInlinesReferencedSchemas(t *testing.T) {
	t.Parallel()
	schema := compile(t)
	// The ShellCheck settings live in schemas/shellcheck, which the
	// generated file references.
	if err := schema.Validate([]byte(`{"tools":{"shellcheck":{"config":{"shell":"bash"}}}}`)); err != nil {
		t.Fatalf("valid ShellCheck settings rejected: %v", err)
	}
	if err := schema.Validate([]byte(`{"tools":{"shellcheck":{"config":{"shell":"cobol"}}}}`)); err == nil {
		t.Fatal("inlined ShellCheck schema accepted an unknown shell")
	}
}

func TestDocumentAddsInstalledProviders(t *testing.T) {
	t.Parallel()
	schema := compile(t, installed(false))
	for _, valid := range []string{
		`{"tools":{"oci-tool":{"config":{"level":"low"}}}}`,
		`{"tools":{"oci-tool":false}}`,
		`{"overrides":[{"includes":["a.yml"],"tools":{"oci-tool":{"config":{"level":"high"}}}}]}`,
	} {
		if err := schema.Validate([]byte(valid)); err != nil {
			t.Errorf("%s rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		`{"tools":{"oci-tool":{"config":{"level":"medium"}}}}`,
		`{"tools":{"oci-tool":{"config":{"unknown":true}}}}`,
		`{"overrides":[{"includes":["a.yml"],"tools":{"oci-tool":{"config":{"level":"medium"}}}}]}`,
		// Only providers that read configuration files accept a path.
		`{"tools":{"oci-tool":{"config":"oci-tool.toml"}}}`,
	} {
		if err := schema.Validate([]byte(invalid)); err == nil {
			t.Errorf("%s accepted", invalid)
		}
	}
	if err := compile(t, installed(true)).Validate([]byte(`{"tools":{"oci-tool":{"config":"oci-tool.toml"}}}`)); err != nil {
		t.Fatalf("configuration file path rejected: %v", err)
	}
}

func TestDocumentScopesProviderSchemasOnce(t *testing.T) {
	t.Parallel()
	manifest := installed(true)
	manifest.ConfigSchema = json.RawMessage(`{"type":"object","$defs":{"limit":{"type":"integer","minimum":2}},"properties":{"value":{"$ref":"#/$defs/limit"}}}`)
	manifest.Process.ConfigSchema = json.RawMessage(`{"$id":"https://example.invalid/process-settings","type":"object","$defs":{"limit":{"type":"integer","maximum":5}},"properties":{"value":{"$ref":"#/$defs/limit"}}}`)
	schema := compile(t, manifest)
	for _, input := range []string{
		`{"tools":{"oci-tool":{"config":{"value":3,"extra":true}}},"overrides":[{"includes":["a.yml"],"tools":{"oci-tool":{"config":{"value":4}}}},{"includes":["b.yml"],"tools":{"oci-tool":{"config":{"value":5}}}}]}`,
		`{"tools":{"oci-tool":{"config":"settings.toml"}}}`,
	} {
		if err := schema.Validate([]byte(input)); err != nil {
			t.Errorf("%s rejected: %v", input, err)
		}
	}
	for _, input := range []string{
		`{"tools":{"oci-tool":{"config":{"value":1}}}}`,
		`{"overrides":[{"includes":["a.yml"],"tools":{"oci-tool":{"config":{"value":6}}}}]}`,
		`{"tools":{"oci-tool":{"config":"bad\npath"}}}`,
	} {
		if err := schema.Validate([]byte(input)); err == nil {
			t.Errorf("%s accepted", input)
		}
	}
}

func TestDocumentShorthandReferences(t *testing.T) {
	t.Parallel()
	manifest := installed(false)
	manifest.ConfigSchema = json.RawMessage(`{"$defs":{"mode":{"enum":["quiet","loud"]}},"type":"object","properties":{"nested":{"type":"object","properties":{"a/b~c":{"$ref":"#/$defs/mode"}},"additionalProperties":false}},"additionalProperties":false}`)
	manifest.ConfigShorthands = map[string]configschema.Shorthand{"mode": {Path: []string{"nested", "a/b~c"}, Precedence: configschema.PrecedenceConfig}}
	schema := compile(t, manifest)
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{`{"tools":{"oci-tool":{"mode":"quiet"}}}`, true},
		{`{"overrides":[{"includes":["**"],"tools":{"oci-tool":{"mode":"invalid"}}}]}`, false},
	} {
		if err := schema.Validate([]byte(tc.value)); (err == nil) != tc.valid {
			t.Errorf("%s: %v", tc.value, err)
		}
	}
}

func TestDocumentPreservesUnevaluatedProperties(t *testing.T) {
	t.Parallel()
	manifest := installed(false)
	manifest.Process = nil
	manifest.ConfigSchema = json.RawMessage(`{"type":"object","allOf":[{"type":"object","properties":{"known":{"type":"integer"}}}],"unevaluatedProperties":false}`)
	schema := compile(t, manifest)
	if err := schema.Validate([]byte(`{"tools":{"oci-tool":{"config":{"known":1}}}}`)); err != nil {
		t.Fatalf("known property rejected: %v", err)
	}
	if err := schema.Validate([]byte(`{"tools":{"oci-tool":{"config":{"unknown":1}}}}`)); err == nil {
		t.Fatal("unevaluated property accepted")
	}
}

func TestDocumentScopesRelativeProviderSchemas(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"", "settings.json", "../settings.json", "/settings.json"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			manifest := installed(false)
			manifest.ConfigSchema = json.RawMessage(fmt.Sprintf(`{"$id":%q,"type":"object","$defs":{"limit":{"type":"integer","minimum":2}},"properties":{"value":{"$ref":"#/$defs/limit"}}}`, id))
			manifest.Process.ConfigSchema = json.RawMessage(fmt.Sprintf(`{"$id":%q,"type":"object","$defs":{"limit":{"type":"integer","maximum":5}},"properties":{"value":{"$ref":"#/$defs/limit"}}}`, id))
			schema := compile(t, manifest)
			if err := schema.Validate([]byte(`{"tools":{"oci-tool":{"config":{"value":3}}}}`)); err != nil {
				t.Fatalf("valid value rejected: %v", err)
			}
			for _, value := range []int{1, 6} {
				if err := schema.Validate(fmt.Appendf(nil, `{"tools":{"oci-tool":{"config":{"value":%d}}}}`, value)); err == nil {
					t.Errorf("invalid value %d accepted", value)
				}
			}
		})
	}
}

func TestDocumentAddsPluginRuleSettingsToSemanticCategory(t *testing.T) {
	t.Parallel()
	manifest := installed(false)
	manifest.Rule.Name = "custom-check"
	manifest.Rule.Category = "security"
	manifest.Rule.OptionsSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","$id":"https://example.com/custom-check-options.schema.json","$defs":{"positive":{"type":"integer","minimum":1}},"properties":{"threshold":{"$ref":"#/$defs/positive"}},"required":["threshold"],"additionalProperties":false}`)
	schema := compile(t, manifest)
	for _, valid := range []string{
		`{"lint":{"rules":{"security":{"custom-check":{"level":"warn","options":{"threshold":2}}}}}}`,
		`{"overrides":[{"includes":["a.yml"],"lint":{"rules":{"security":{"custom-check":{"level":"error","options":{"threshold":3}}}}}}]}`,
	} {
		if err := schema.Validate([]byte(valid)); err != nil {
			t.Errorf("%s rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		`{"lint":{"rules":{"security":{"custom-check":{"level":"warn","options":{"threshold":0}}}}}}`,
		`{"lint":{"rules":{"correctness":{"custom-check":"off"}}}}`,
	} {
		if err := schema.Validate([]byte(invalid)); err == nil {
			t.Errorf("%s accepted", invalid)
		}
	}
}

func TestDocumentRejectsUnsupportedRuleCategory(t *testing.T) {
	t.Parallel()
	manifest := installed(false)
	manifest.Rule.Category = "external"
	if _, err := Document(moduleRoot, []pluginmanifests.Manifest{manifest}); err == nil || !strings.Contains(err.Error(), `unsupported category "external"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestDocumentUsesInstalledReplacementSettings(t *testing.T) {
	t.Parallel()
	manifest := installed(false)
	manifest.Rule.Name = "shellcheck"
	manifest.Rule.Category = "correctness"
	schema := compile(t, manifest)
	if err := schema.Validate([]byte(`{"tools":{"shellcheck":{"config":{"level":"low"}}}}`)); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate([]byte(`{"tools":{"shellcheck":{"config":{"shell":"bash"}}}}`)); err == nil {
		t.Fatal("accepted settings from replaced provider")
	}
}

func TestDocumentRefusesReferenceCycle(t *testing.T) {
	t.Parallel()
	files := fstest.MapFS{
		RootFile: {Data: []byte(`{"properties":{"tools":{"oneOf":[{"type":"object","properties":{},"additionalProperties":{}}]}},"$ref":"a.json"}`)},
		"a.json": {Data: []byte(`{"$ref":"b.json"}`)},
		"b.json": {Data: []byte(`{"$ref":"a.json"}`)},
	}
	read := func(name string) ([]byte, error) { return fs.ReadFile(files, name) }
	if _, err := Document(read, nil); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v", err)
	}
}
