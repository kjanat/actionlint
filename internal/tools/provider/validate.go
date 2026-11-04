package provider

import (
	_ "embed"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/rule"
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
	// Accept old rule-level fix labels only at the input boundary. They do not
	// describe whether any particular finding has an edit.
	var wire struct {
		Manifest
		Rule struct {
			rule.Descriptor
			LegacyFixAvailability string `json:"fix_availability,omitempty"`
		} `json:"rule"`
	}
	if err := jsonv2.Unmarshal(data, &wire, jsonv2.RejectUnknownMembers(true)); err != nil {
		return Manifest{}, fmt.Errorf("invalid tool provider manifest: %w", err)
	}
	manifest := wire.Manifest
	manifest.Rule = wire.Rule.Descriptor
	if err := validate(manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid tool provider manifest %s: %w", manifest.ID, err)
	}
	return manifest, nil
}

// validate checks what the schema cannot express.
func validate(m Manifest) error {
	if !rule.ValidCategory(m.Rule.Category) {
		return fmt.Errorf("rule %q has invalid category %q; expected one of %s", m.Rule.Name, m.Rule.Category, strings.Join(rule.Categories(), ", "))
	}
	seenFacts := map[analyzer.InputKind]bool{}
	for _, fact := range m.RequiredInputs {
		if fact != analyzer.Source && fact != analyzer.Dependencies || seenFacts[fact] {
			return fmt.Errorf("unsupported or duplicate document input kind %q", fact)
		}
		seenFacts[fact] = true
	}
	if m.Role != RoleDocument && (len(m.RequiredInputs) != 0 || len(m.Selectors.Documents) != 0) {
		return errors.New("document input kinds and selectors require the document role")
	}
	if m.Role == RoleDocument && (len(m.Selectors.Inputs) != 0 || len(m.Selectors.Shells) != 0) {
		return errors.New("document providers cannot select script inputs or shells")
	}
	if err := analyzer.ValidateScriptInputs(m.Selectors.Inputs); err != nil {
		return err
	}
	if err := validateConfigSchema("config_schema", m.ConfigSchema); err != nil {
		return err
	}
	if err := validateConfigShorthands(m); err != nil {
		return err
	}
	if m.Process == nil {
		return nil
	}
	p := m.Process
	if err := p.ValidateProgram(); err != nil {
		return err
	}
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
	if err := validateRendering(p); err != nil {
		return err
	}
	if err := validateLaunchers(p.UserArgs); err != nil {
		return err
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

// configPlaceholders maps each configuration placeholder to the rendering it
// needs.
var configPlaceholders = map[string]Render{
	"{config:toml-file}": RenderTOMLFile,
	"{config:json-file}": RenderJSONFile,
}

// validateRendering requires every configuration placeholder to match the
// configuration rendering and a rendering that needs a placeholder or the
// request on stdin to have it.
func validateRendering(p *ProcessSpec) error {
	used := map[Render]bool{}
	for _, arg := range p.Args {
		render, ok := configPlaceholders[arg]
		if strings.HasPrefix(arg, "{config:toml-pairs:") {
			render, ok = RenderTOMLPairs, true
		}
		if !ok {
			continue
		}
		if render != p.Config.Render {
			return fmt.Errorf("process.args: %s needs config.render %q, not %q", arg, render, p.Config.Render)
		}
		if used[render] {
			return fmt.Errorf("process.args: %s appears more than once", arg)
		}
		used[render] = true
	}
	switch p.Config.Render {
	case RenderNone:
	case RenderJSONStdin:
		if p.Stdin != StdinRequest {
			return errors.New(`process.config.render "json-stdin" needs process.stdin "request"`)
		}
	case RenderJSONFile, RenderTOMLFile, RenderTOMLPairs:
		if !used[p.Config.Render] {
			return fmt.Errorf("process.config.render %q has no placeholder in process.args", p.Config.Render)
		}
	default:
		return fmt.Errorf("unknown process.config.render %q", p.Config.Render)
	}
	return nil
}

// validateLaunchers compiles every launcher expression.
func validateLaunchers(args *UserArgs) error {
	if args == nil {
		return nil
	}
	for i, launcher := range args.Launchers {
		for field, expression := range map[string]string{"command": launcher.Command, "selector": launcher.Selector, "tool": launcher.Tool} {
			if expression == "" {
				continue
			}
			if _, err := regexp.Compile(expression); err != nil {
				return fmt.Errorf("process.user_args.launchers[%d].%s: %w", i, field, err)
			}
		}
		for option := range launcher.Choices {
			if !slices.Contains(launcher.ValueOptions, option) {
				return fmt.Errorf("process.user_args.launchers[%d].choices: %s is not a value option", i, option)
			}
		}
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
