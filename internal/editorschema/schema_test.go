package editorschema

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/configschema"
	"actionlint.kjanat.dev/v2/rule"
)

// moduleRoot reads the generated actionlint.schema.json and schemas/.
func moduleRoot(name string) ([]byte, error) { return fs.ReadFile(os.DirFS("../.."), name) }

// installed is a tool provider a plugin installs. Its settings accept only
// level: low or high.
func installed(configFiles bool) provider.Manifest {
	manifest := provider.Manifest{
		Rule:         rule.Descriptor{Name: "oci-tool", Description: "A tool from an OCI registry"},
		ConfigSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"level":{"enum":["low","high"]}},"additionalProperties":false}`),
		Process:      &provider.ProcessSpec{},
	}
	if configFiles {
		manifest.Process.ConfigFiles = &provider.ConfigFiles{Names: []string{"oci-tool.toml"}}
	}
	return manifest
}

func compile(t *testing.T, manifests ...provider.Manifest) *configschema.Schema {
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

// Configuration validates a bundled tool's settings against the bundled
// manifest, so an installed provider with that rule name keeps the bundled
// entry.
func TestDocumentKeepsBundledEntries(t *testing.T) {
	manifest := installed(false)
	manifest.Rule.Name = "shellcheck"
	if err := compile(t, manifest).Validate([]byte(`{"tools":{"shellcheck":{"config":{"shell":"bash"}}}}`)); err != nil {
		t.Fatalf("bundled ShellCheck entry replaced: %v", err)
	}
}

func TestDocumentRefusesReferenceCycle(t *testing.T) {
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
