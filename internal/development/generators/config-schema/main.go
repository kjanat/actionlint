// Generate an editor schema from the configuration types and their Go comments.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"unicode"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	genfile "actionlint.kjanat.dev/v2/internal/development/generators/files"
	actionlint "actionlint.kjanat.dev/v2/internal/input/configuration"
	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
	toolconfig "actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	"actionlint.kjanat.dev/v2/internal/model/outputformat"

	"github.com/invopop/jsonschema"
)

func reflector() *jsonschema.Reflector {
	r := &jsonschema.Reflector{
		FieldNameTag:               "yaml",
		RequiredFromJSONSchemaTags: true,
		// Shared wire shapes use explicit, stable $defs through mapYAMLType.
		// Other reflected wrappers do not need generated Go type definitions.
		DoNotReference: true,
	}
	r.Mapper = func(t reflect.Type) *jsonschema.Schema {
		return mapYAMLType(t, r.LookupComment)
	}
	return r
}

// mapYAMLType describes the wire format of types with custom UnmarshalYAML
// methods. Ordinary fields, including future config settings, use reflection.
func mapYAMLType(t reflect.Type, lookupComment func(reflect.Type, string) string) *jsonschema.Schema {
	reflectMapping := func(value any) *jsonschema.Schema {
		r := reflector()
		r.LookupComment = lookupComment
		schema := r.Reflect(value)
		schema.Version, schema.ID = "", ""
		return schema
	}
	switch t {
	case reflect.TypeFor[actionlint.MetadataMirrorServices]():
		return &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string", Enum: []any{"jsdelivr"}}}
	case reflect.TypeFor[map[string]*string]():
		return &jsonschema.Schema{Type: "object", PropertyNames: definitionRef("non-blank-string"), AdditionalProperties: &jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string"}, {Type: "null"}}}, Extras: map[string]any{"x-tombi-additional-key-label": "variable_name"}}
	case reflect.TypeFor[actionlint.LocalActionSources]():
		return &jsonschema.Schema{Type: "object", PropertyNames: definitionRef("local-action-directory"), AdditionalProperties: definitionRef("local-action-directory"), Extras: map[string]any{"x-tombi-additional-key-label": "action_directory"}}
	case reflect.TypeFor[actionlint.PluginDeclarations]():
		mapping := reflectMapping(struct {
			Name     string `yaml:"name" jsonschema:"required,minLength=1,description=Registry-assigned alias or plugin repository: owner/repo or github.com/owner/repo or gitlab.com/group/repo."`
			Version  string `yaml:"version,omitempty" jsonschema:"minLength=1,description=Exact release tag or latest stable. Omission selects latest."`
			Registry string `yaml:"registry,omitempty" jsonschema:"minLength=1,description=HTTPS catalogue or absolute file URI for an unpublished local candidate. Omission uses the public actionlint registry."`
			SHA256   string `yaml:"sha256,omitempty" jsonschema:"pattern=^[a-fA-F0-9]{64}$,description=Verified SHA256 digest binding an exact version from a local file candidate registry. Not accepted for HTTPS registries."`
		}{})
		direct := reflectMapping(struct {
			Source string `yaml:"source" jsonschema:"required,minLength=1,description=Independent artifact URL with a mandatory SHA256 pin, or an OCI digest reference. Cannot be combined with registry declaration fields."`
		}{})
		source, _ := direct.Properties.Get("source")
		source.Pattern = `^(https://.+#sha256=[a-fA-F0-9]{64}|oci://.+@sha256:[a-f0-9]{64})$`
		return &jsonschema.Schema{
			Type:        "array",
			UniqueItems: true,
			Items: &jsonschema.Schema{OneOf: []*jsonschema.Schema{
				{Type: "string", MinLength: new(uint64(1)), Not: &jsonschema.Schema{Pattern: `@$`}, Description: "Plugin alias, repository with optional release tag, HTTPS artifact URL pinned with #sha256=<64 hex digits>, or OCI reference pinned with @sha256:<64 hex digits>.", Examples: []any{"shellcheck", "kjanat/actionlint-plugin-shellcheck@v0.2.0", "gitlab.com/group/plugin", "https://example.com/plugin.wasm#sha256=" + strings.Repeat("a", 64), "oci://ghcr.io/owner/provider@sha256:" + strings.Repeat("a", 64)}},
				mapping,
				direct,
			}},
		}
	case reflect.TypeFor[actionlint.IgnoreConfig]():
		mapping := reflectMapping(struct {
			Rule   string `yaml:"rule" jsonschema:"required,minLength=1,description=Built-in or registered plugin diagnostic rule to suppress. Registration is validated when loading configuration."`
			File   string `yaml:"file,omitempty" jsonschema:"minLength=1,description=Reported file path or doublestar glob relative to the repository. Matches sourced scripts when diagnostics point to them."`
			Uses   string `yaml:"uses,omitempty" jsonschema:"minLength=1,description=Exact action or reusable workflow reference including its version."`
			Job    string `yaml:"job,omitempty" jsonschema:"minLength=1,description=Exact workflow job ID."`
			Step   string `yaml:"step,omitempty" jsonschema:"minLength=1,description=Exact step ID or name."`
			Reason string `yaml:"reason,omitempty" jsonschema:"minLength=1,description=Optional explanation for this exception."`
		}{})
		rule, _ := mapping.Properties.Get("rule")
		rule.AllOf = []*jsonschema.Schema{definitionRef("rule-name")}
		rule.Type, rule.MinLength = "", nil
		entry, _ := checking.ConfigurationRules().FindRule("forbidden-suppression")
		blocked := []any{entry.Name}
		for _, alias := range entry.Aliases {
			blocked = append(blocked, alias.Name)
		}
		rule.Not = &jsonschema.Schema{Enum: blocked}
		for _, name := range []string{"file", "uses", "job", "step", "reason"} {
			property, _ := mapping.Properties.Get(name)
			property.Ref = "#/$defs/non-blank-string"
			property.Type, property.MinLength = "", nil
		}
		for _, selector := range []string{"file", "uses", "job", "step"} {
			mapping.AnyOf = append(mapping.AnyOf, &jsonschema.Schema{Required: []string{selector}})
		}
		return mapping
	case reflect.TypeFor[actionlint.FilesConfig]():
		mapping := reflectMapping(struct {
			Includes []string `yaml:"includes" jsonschema:"nullable,description=Repository-relative doublestar globs. Omitted or null selects all files; an empty array selects none. A leading ! excludes a match."`
			Excludes []string `yaml:"excludes" jsonschema:"nullable,description=Repository-relative exclusions. Exclusions always win over includes."`
		}{})
		mapping.Version, mapping.ID = "", ""
		return mapping
	case reflect.TypeFor[actionlint.Level]():
		return definitionRef("level")
	case reflect.TypeFor[actionlint.Preset]():
		return definitionRef("preset")
	case reflect.TypeFor[actionlint.LintRulesConfig]():
		mapping := reflectMapping(struct {
			Preset  actionlint.Preset `yaml:"preset" jsonschema:"description=Baseline selection: recommended, all stable rules, or none. Explicit rules override presets."`
			Disable []string          `yaml:"disable" jsonschema:"description=Suppress these diagnostic rule IDs at every severity. Omission or an empty list suppresses none."`
		}{})
		disable, _ := mapping.Properties.Get("disable")
		disable.Items = definitionRef("rule-name")
		disable.Type = ""
		disable.OneOf = []*jsonschema.Schema{{Type: "array"}, {Type: "null"}}
		categories := lintconfig.Categories()
		knownByCategory := make(map[string][]string, len(categories))
		knownNames := make([]string, 0)
		for _, rule := range checking.ConfigurationRules().Rules() {
			knownNames = append(knownNames, rule.Name)
			knownByCategory[rule.Category] = append(knownByCategory[rule.Category], rule.Name)
			for _, alias := range rule.Aliases {
				knownNames = append(knownNames, alias.Name)
				knownByCategory[rule.Category] = append(knownByCategory[rule.Category], alias.Name)
			}
		}
		for _, category := range categories {
			group := reflectMapping(struct {
				Preset actionlint.Preset `yaml:"preset" jsonschema:"description=Baseline selection within this group: recommended, all, or none."`
				Level  actionlint.Level  `yaml:"level" jsonschema:"description=Default diagnostic level for rules in this group."`
			}{})
			group.Version, group.ID = "", ""
			group.Extras = map[string]any{"x-tombi-additional-key-label": "rule_name"}
			for _, rule := range checking.ConfigurationRules().Rules() {
				if rule.Category != category {
					continue
				}
				setting := &jsonschema.Schema{AllOf: []*jsonschema.Schema{definitionRef("rule-setting")}}
				if options := ruleOptions(rule.Name); options != nil {
					optionsSchema := reflectMapping(options)
					optionsSchema.Version, optionsSchema.ID = "", ""
					setting = settingWithOptions(&jsonschema.Schema{Description: "Options specific to this rule; null resets to defaults.", AnyOf: []*jsonschema.Schema{{Type: "null"}, {AllOf: []*jsonschema.Schema{{Type: "object", AdditionalProperties: jsonschema.TrueSchema}, optionsSchema}}}})
				}
				description := rule.Description
				if rule.Experimental() {
					description += ". Nursery; disabled by default and excluded from stable presets."
				} else if !rule.Recommended {
					description += ". Stable; not enabled by the recommended preset."
				}
				setting.Description = description
				group.Properties.Set(rule.Name, setting)
				for _, alias := range rule.Aliases {
					aliasSetting := *setting
					aliasSetting.Deprecated = alias.Deprecated
					aliasSetting.Description = "Alias for " + rule.Name + "."
					if alias.Deprecated {
						aliasSetting.Description = "Deprecated alias. Use " + rule.Name + " instead."
					}
					group.Properties.Set(alias.Name, &aliasSetting)
				}
			}
			otherCategoryNames := make([]any, 0, len(knownNames))
			for _, name := range knownNames {
				if !slices.Contains(knownByCategory[category], name) {
					otherCategoryNames = append(otherCategoryNames, name)
				}
			}
			unknown := definitionRef("plugin-setting")
			if len(otherCategoryNames) != 0 {
				group.PropertyNames = &jsonschema.Schema{Not: &jsonschema.Schema{Enum: otherCategoryNames}}
			}
			group.AdditionalProperties = unknown
			level := definitionRef("group-level")
			description := "Rules concerning " + category + ". Use true/on to enable the group, false/off to disable it, all for its full preset, or a mapping for individual rules. Group settings override the root preset; individual settings override the group."
			mapping.Properties.Set(category, &jsonschema.Schema{Description: description, OneOf: []*jsonschema.Schema{level, group}})
		}
		mapping.Version, mapping.ID = "", ""
		return mapping
	case reflect.TypeFor[actionlint.ToolsConfig]():
		return toolsSchema(reflectMapping)
	case reflect.TypeFor[actionlint.OutputFormats]():
		values := make([]any, 0, len(outputformat.Names()))
		for _, name := range outputformat.Names() {
			values = append(values, name)
		}
		return &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string", Enum: values}}
	case reflect.TypeFor[actionlint.IgnorePatterns]():
		// JSON Schema's regex format uses a different dialect from Go's regexp.
		return &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}}
	case reflect.TypeFor[map[string]actionlint.IgnorePatterns]():
		return &jsonschema.Schema{
			Type:   "object",
			Extras: map[string]any{"x-tombi-additional-key-label": "file_pattern"},
			AdditionalProperties: &jsonschema.Schema{OneOf: []*jsonschema.Schema{
				{Type: "array", Items: &jsonschema.Schema{Type: "string"}},
				{Type: "null"},
			}},
		}
	case reflect.TypeFor[actionlint.RepositoryConfig]():
		mapping := reflectMapping(struct {
			Permissions  *string            `yaml:"default-workflow-permissions" jsonschema:"nullable,enum=read,enum=write,description=Repository default workflow token permissions. Omission inherits host knowledge; null makes the default unknown."`
			Runners      []string           `yaml:"runners" jsonschema:"nullable,description=Additional runner labels or Go path.Match patterns. Empty permits no additional labels."`
			Variables    map[string]*string `yaml:"variables" jsonschema:"nullable,description=Available variable names with optional string values. Null values retain known names without assuming values. Values are never included in diagnostics."`
			Secrets      []string           `yaml:"secrets" jsonschema:"nullable,description=Available secret names. Values are never accepted. Empty permits only built-in and workflow_call secrets."`
			Environments map[string]struct {
				Variables map[string]*string `yaml:"variables" jsonschema:"nullable,description=Available variable names with optional string values for this environment. Null values retain known names without assuming values. Values are never included in diagnostics."`
				Secrets   []string           `yaml:"secrets" jsonschema:"nullable,description=Available secret names for this environment. Values are never accepted."`
			} `yaml:"environments" jsonschema:"nullable,description=Known deployment environments and their variable and secret names."`
		}{})
		mapping.Version, mapping.ID = "", ""
		for _, name := range []string{"runners", "secrets"} {
			property, _ := mapping.Properties.Get(name)
			visitSchemaType(property, "array", func(array *jsonschema.Schema) {
				array.Items = definitionRef("non-blank-string")
				array.UniqueItems = true
			})
		}
		environments, _ := mapping.Properties.Get("environments")
		environments.Extras = map[string]any{"x-tombi-additional-key-label": "environment_name"}
		visitSchemaType(environments, "object", func(environments *jsonschema.Schema) {
			environments.PropertyNames = definitionRef("non-blank-string")
			environments.Extras = map[string]any{"x-tombi-additional-key-label": "environment_name"}
			secrets, _ := environments.AdditionalProperties.Properties.Get("secrets")
			visitSchemaType(secrets, "array", func(array *jsonschema.Schema) {
				array.Items = definitionRef("non-blank-string")
				array.UniqueItems = true
			})
		})
		return mapping
	case reflect.TypeFor[actionlint.JobTimeoutPolicy]():
		// The runtime type stores private state and accepts either a boolean or
		// this mapping. Keep this in sync with JobTimeoutPolicy.UnmarshalYAML.
		mapping := reflectMapping(struct {
			MinMinutes float64 `yaml:"min-minutes" jsonschema:"exclusiveMinimum=0,description=Smallest allowed job timeout in minutes. Must not exceed max-minutes."`
			MaxMinutes float64 `yaml:"max-minutes" jsonschema:"exclusiveMinimum=0,description=Largest allowed job timeout in minutes."`
		}{})
		mapping.Version = ""
		mapping.ID = ""
		return &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "boolean"}, mapping}}
	case reflect.TypeFor[lintconfig.CommitHashOptions]():
		// The runtime type keeps validated patterns private. Keep this in sync
		// with CommitHashOptions.UnmarshalYAML.
		mapping := reflectMapping(struct {
			Exclude []string `yaml:"exclude" jsonschema:"nullable"`
		}{})
		exclude, _ := mapping.Properties.Get("exclude")
		exclude.Description = "Contains repository, action-path, or docker://image glob patterns. Repository patterns such as my-org/* also cover actions and workflows within matching repositories. Include @ref to restrict an exception to that ref."
		mapping.Description = "Selects exceptions to immutable action references."
		mapping.Version = ""
		mapping.ID = ""
		return mapping
	case reflect.TypeFor[toolconfig.Backend]():
		return definitionRef("backend")
	case reflect.TypeFor[actionlint.PermissionLocationList]():
		values := []any{}
		for _, location := range actionlint.PermissionLocations() {
			values = append(values, string(location))
		}
		location := &jsonschema.Schema{Type: "string", Enum: values}
		return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{location, {Type: "array", MinItems: new(uint64(1)), Items: location}}, Description: "Require permissions at every listed location. Use workflow, every-job, or a nonempty array. Omit require-at to accept permissions on the workflow or on every job. Set missing-permissions to off to disable the rule."}
	case reflect.TypeFor[actionlint.PermissionsPolicy]():
		mapping := reflectMapping(struct {
			Scope string `yaml:"scope" jsonschema:"enum=workflow,enum=job,default=workflow,description=Require a workflow-level declaration or a declaration on every job."`
		}{})
		mapping.Version = ""
		mapping.ID = ""
		return &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "boolean"}, mapping}}
	case reflect.TypeFor[actionlint.SuppressionsPolicy]():
		mapping := reflectMapping(struct {
			Rules  []string `yaml:"rules" jsonschema:"minItems=1,description=Rule IDs whose inline exceptions are prohibited. Omit to select all suppressible rules."`
			Report string   `yaml:"report" jsonschema:"enum=suppression,enum=violation,enum=all,default=all,description=Select suppression comments, original violations, or both for reporting."`
		}{})
		rules, _ := mapping.Properties.Get("rules")
		rules.Items = definitionRef("rule-name")
		mapping.Version = ""
		mapping.ID = ""
		return &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "boolean"}, mapping}}
	default:
		return nil
	}
}

func visitSchemaType(schema *jsonschema.Schema, kind string, visit func(*jsonschema.Schema)) {
	if schema.Type == kind {
		visit(schema)
	}
	for _, alternatives := range [][]*jsonschema.Schema{schema.OneOf, schema.AnyOf, schema.AllOf} {
		for _, alternative := range alternatives {
			visitSchemaType(alternative, kind, visit)
		}
	}
}

func backendSchema(reflectMapping func(any) *jsonschema.Schema) *jsonschema.Schema {
	// This schema describes the process and wasm fields read by Backend.DecodeConfig.
	mapping := reflectMapping(struct {
		Process *struct {
			Executable string            `yaml:"executable,omitempty" jsonschema:"description=A command on PATH or a literal path. Relative paths resolve from the declaring config file; inline configs use the analysis directory."`
			Args       []string          `yaml:"args,omitempty" jsonschema:"nullable,description=Literal arguments prepended to actionlint's analysis arguments."`
			Env        map[string]string `yaml:"env,omitempty" jsonschema:"nullable,description=Overrides named variables in the child process only."`
		} `yaml:"process,omitempty" jsonschema:"description=Configures native executable invocation."`
		Wasm *struct {
			Path string `yaml:"path" jsonschema:"required,minLength=1"`
		} `yaml:"wasm,omitempty" jsonschema:"description=Selects a core WebAssembly engine using the versioned core JSON ABI."`
	}{})
	wasm, _ := mapping.Properties.Get("wasm")
	path, _ := wasm.Properties.Get("path")
	path.AllOf = []*jsonschema.Schema{definitionRef("nul-free-string")}
	path.Description = "A raw or gzip-compressed module path, file URI, HTTP(S) URL, or oci://<host>/<repository>@sha256:<hex> reference to an image with a WebAssembly layer. URI fragments may pin downloaded bytes with #sha256=<hex>. Relative paths resolve from the declaring config file; inline configs use the analysis directory."
	process, _ := mapping.Properties.Get("process")
	executable, _ := process.Properties.Get("executable")
	executable.AllOf = []*jsonschema.Schema{definitionRef("nul-free-string")}
	args, _ := process.Properties.Get("args")
	for _, alternative := range args.OneOf {
		if alternative.Type == "array" {
			alternative.Items = definitionRef("nul-free-string")
		}
	}
	env, _ := process.Properties.Get("env")
	for _, alternative := range env.OneOf {
		if alternative.Type == "object" {
			alternative.PropertyNames = &jsonschema.Schema{MinLength: new(uint64(1)), Not: &jsonschema.Schema{Pattern: `[=\x00]`}}
			alternative.AdditionalProperties = definitionRef("nul-free-string")
		}
	}
	for _, name := range []string{"process", "wasm"} {
		transport, _ := mapping.Properties.Get(name)
		mapping.Properties.Set(name, &jsonschema.Schema{OneOf: []*jsonschema.Schema{transport, {Type: "null"}}})
		selected := jsonschema.NewProperties()
		selected.Set(name, &jsonschema.Schema{Type: "object", Description: transport.Description, AdditionalProperties: jsonschema.TrueSchema})
		mapping.OneOf = append(mapping.OneOf, &jsonschema.Schema{Required: []string{name}, Properties: selected, AdditionalProperties: jsonschema.TrueSchema})
	}
	mapping.Version = ""
	mapping.ID = ""
	return mapping
}

// documentFields keeps property documentation visible regardless of which
// nullable branch matches, including a null value or an unfinished YAML value.
func documentFields(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.OneOf) == 2 && s.OneOf[1].Type == "null" && s.Description == "" {
		s.Description = s.OneOf[0].Description
		s.OneOf[0].Description = ""
	}
	if s.Description != "" {
		paragraphs := strings.Split(s.Description, "\n\n")
		for i, paragraph := range paragraphs {
			paragraphs[i] = strings.ReplaceAll(paragraph, "\n", " ")
		}
		s.Description = strings.Join(paragraphs, "\n\n")
		if s.Extras == nil {
			s.Extras = make(map[string]any)
		}
		if _, exists := s.Extras["markdownDescription"]; !exists {
			s.Extras["markdownDescription"] = s.Description
		}
		if s.Deprecated {
			s.Extras["deprecationMessage"] = s.Description
		}
	}
	if s.Properties != nil {
		for name, property := range s.Properties.FromOldest() {
			property.Title = name
			documentFields(property)
		}
	}
	for _, variant := range s.OneOf {
		documentFields(variant)
	}
	for _, variant := range s.AnyOf {
		documentFields(variant)
	}
	for _, variant := range s.AllOf {
		documentFields(variant)
	}
	documentFields(s.Then)
	documentFields(s.Items)
	documentFields(s.AdditionalProperties)
	for _, definition := range s.Definitions {
		documentFields(definition)
	}
}

func documentedReflector() (*jsonschema.Reflector, error) {
	r := reflector()
	for _, file := range []string{"config.go", "lint.go", "reporting.go", "network.go", "rule_settings.go", "catalog.go", "file_overrides.go", "repository.go", "knowledge_options.go"} {
		if err := r.AddGoComments("actionlint.kjanat.dev/v2", filepath.Join("internal", "input", "configuration", file), jsonschema.WithFullComment()); err != nil {
			return nil, fmt.Errorf("read config comments: %w", err)
		}
	}
	// AddGoComments constructs package keys from filesystem paths, whereas
	// reflection uses slash-separated import paths on every platform.
	comments := make(map[string]string, len(r.CommentMap))
	for key, comment := range r.CommentMap {
		comments[filepath.ToSlash(key)] = comment
	}
	r.CommentMap = comments
	r.LookupComment = func(t reflect.Type, field string) string {
		key := t.PkgPath() + "." + t.Name()
		name := t.Name()
		if field != "" {
			key += "." + field
			name = field
		}
		text := []rune(strings.TrimPrefix(r.CommentMap[key], name+" "))
		if len(text) > 0 {
			text[0] = unicode.ToUpper(text[0])
		}
		return string(text)
	}
	return r, nil
}

func encodeSchema(s *jsonschema.Schema) ([]byte, error) {
	documentFields(s)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode schema: %w", err)
	}
	return append(b, '\n'), nil
}

// generate runs from the repository root, as go generate does.
func generate() ([]byte, error) {
	r, err := documentedReflector()
	if err != nil {
		return nil, err
	}
	s := r.Reflect(actionlint.Config{})
	s.Type = ""
	s.AnyOf = []*jsonschema.Schema{{Type: "object", AdditionalProperties: jsonschema.TrueSchema}, {Type: "null"}}
	overrides, _ := s.Properties.Get("overrides")
	visitSchemaType(overrides, "array", func(array *jsonschema.Schema) {
		includes, _ := array.Items.Properties.Get("includes")
		includes.Contains = &jsonschema.Schema{Type: "string", Not: &jsonschema.Schema{Pattern: "^!"}}
	})
	s.Definitions, err = sharedDefinitions(func(value any) *jsonschema.Schema {
		schema := r.Reflect(value)
		schema.Version, schema.ID = "", ""
		return schema
	})
	if err != nil {
		return nil, err
	}
	s.Definitions["non-blank-string"] = &jsonschema.Schema{
		Type: "string", MinLength: new(uint64(1)),
		Pattern: "[^\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]",
	}
	// Accept editor metadata without listing it among completion candidates.
	s.PatternProperties = map[string]*jsonschema.Schema{
		`^\$schema$`: {Type: "string", Description: "Schema reference for editors. actionlint ignores it."},
	}
	// Resolve tool schemas beside this document in Git checkouts and npm packages.
	s.ID = ""
	s.Title = "actionlint configuration"
	s.Extras = map[string]any{
		"x-tombi-toml-version":     "v1.1.0",
		"x-tombi-table-keys-order": map[string]string{"properties": "schema"},
	}
	s.Comments = "Generated from internal/input/configuration/config.go by go generate -run generate-config-schema. DO NOT EDIT."
	return encodeSchema(s)
}

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/development/generators/config-schema")
		os.Exit(1)
	}
	b, err := generate()
	if err == nil {
		err = genfile.Write(context.Background(), "actionlint.schema.json", b, false)
	}
	if err == nil {
		err = writeEmbeddedSchemas()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeEmbeddedSchemas() error {
	paths, err := filepath.Glob("schemas/*/*.schema.json")
	if err != nil {
		return err
	}
	paths = append(paths, "actionlint.schema.json")
	for _, name := range paths {
		content, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		destination := filepath.Join("internal/input/configuration/schema/assets", name)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(destination, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func ruleNames() []string {
	rules := checking.ConfigurationRules().Rules()
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, rule.Name)
	}
	return names
}

func ruleOptions(name string) any {
	if rule, ok := checking.ConfigurationRules().FindRule(name); ok && rule.Options != nil {
		return rule.Options()
	}
	return nil
}
