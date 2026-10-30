package provider

import (
	_ "embed"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"actionlint.kjanat.dev/v2/analyzer"
)

// maxManifestBytes bounds a manifest before it is decoded.
const maxManifestBytes = 1 << 20

//go:embed manifest_schema.json
var manifestSchemaJSON []byte

// manifestSchema compiles the embedded schema once. A failure is a build
// defect, reported by every Parse call and by the package tests.
var manifestSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileSchema("https://actionlint.kjanat.dev/schemas/tool-provider-manifest-1.json", manifestSchemaJSON)
})

// Parse decodes and validates one manifest. Duplicate object keys, unknown
// members and values outside the schema are rejected; so are selectors,
// template effects, version patterns and configuration schemas the host
// could not use.
func Parse(data []byte) (Manifest, error) {
	if len(data) > maxManifestBytes {
		return Manifest{}, fmt.Errorf("tool provider manifest exceeds %d bytes", maxManifestBytes)
	}
	schema, err := manifestSchema()
	if err != nil {
		return Manifest{}, fmt.Errorf("tool provider manifest schema: %w", err)
	}
	var document any
	if err := jsonv2.Unmarshal(data, &document); err != nil {
		return Manifest{}, fmt.Errorf("invalid tool provider manifest JSON: %w", err)
	}
	if err := schema.Validate(document); err != nil {
		return Manifest{}, fmt.Errorf("invalid tool provider manifest: %w", err)
	}
	var manifest Manifest
	if err := jsonv2.Unmarshal(data, &manifest, jsonv2.RejectUnknownMembers(true)); err != nil {
		return Manifest{}, fmt.Errorf("invalid tool provider manifest: %w", err)
	}
	if err := validate(manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid tool provider manifest %s: %w", manifest.ID, err)
	}
	return manifest, nil
}

// validate checks what the schema cannot express.
func validate(m Manifest) error {
	if err := analyzer.ValidateScriptInputs(m.Selectors.Inputs); err != nil {
		return err
	}
	if err := validateConfigSchema("config_schema", m.ConfigSchema); err != nil {
		return err
	}
	if m.Process == nil {
		return nil
	}
	p := m.Process
	if err := validateConfigSchema("process.config_schema", p.ConfigSchema); err != nil {
		return err
	}
	if p.Version != nil {
		pattern, err := regexp.Compile(p.Version.Pattern)
		if err != nil {
			return fmt.Errorf("process.version.pattern: %w", err)
		}
		if pattern.NumSubexp() != 1 {
			return fmt.Errorf("process.version.pattern must have exactly one capture group, has %d", pattern.NumSubexp())
		}
	}
	if len(p.TemplateEffects) == 0 {
		return nil
	}
	if p.Version == nil || p.Version.TemplateEffects == "" {
		return errors.New("process.template_effects requires process.version.template_effects, the reviewed engine version")
	}
	if m.Capabilities.TemplateContract != analyzer.TemplateEffectsContract {
		return fmt.Errorf("process.template_effects requires capabilities.template_contract %q", analyzer.TemplateEffectsContract)
	}
	for code, effect := range p.TemplateEffects {
		if !effect.Valid() {
			return fmt.Errorf("process.template_effects.%s: unknown template effect %q", code, effect)
		}
	}
	return nil
}

// validateConfigSchema compiles an engine configuration schema without
// granting it filesystem or network reads.
func validateConfigSchema(field string, schema json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	if _, err := compileSchema("https://actionlint.invalid/provider-config.json", schema); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

func compileSchema(url string, data []byte) (*jsonschema.Schema, error) {
	var document any
	if err := jsonv2.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(bundledSchemaLoader{})
	if err := compiler.AddResource(url, document); err != nil {
		return nil, err
	}
	return compiler.Compile(url)
}

// bundledSchemaLoader refuses every external reference: schemas must bundle
// their definitions.
type bundledSchemaLoader struct{}

func (bundledSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("schema reference %q must be bundled", url)
}
