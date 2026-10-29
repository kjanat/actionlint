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

	actionlint "actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/genfile"

	"github.com/invopop/jsonschema"
)

const (
	shellcheckSchemaVersion = "0.11.0"
	shellcheckSchemaPath    = "schemas/shellcheck/" + shellcheckSchemaVersion + ".schema.json"
)

func reflector() *jsonschema.Reflector {
	r := &jsonschema.Reflector{
		FieldNameTag:               "yaml",
		RequiredFromJSONSchemaTags: true,
		DoNotReference:             true,
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
	case reflect.TypeFor[actionlint.PluginDeclarations]():
		mapping := reflectMapping(struct {
			Name     string `yaml:"name" jsonschema:"required,minLength=1,description=Registry-assigned alias or plugin repository: owner/repo or github.com/owner/repo or gitlab.com/group/repo."`
			Version  string `yaml:"version,omitempty" jsonschema:"minLength=1,description=Exact release tag or latest stable. Omission selects latest."`
			Registry string `yaml:"registry,omitempty" jsonschema:"minLength=1,description=HTTPS catalogue or absolute file URI for an unpublished local candidate. Omission uses the public actionlint registry."`
		}{})
		return &jsonschema.Schema{
			Type: "array",
			Items: &jsonschema.Schema{OneOf: []*jsonschema.Schema{
				{Type: "string", MinLength: new(uint64(1)), Description: "Plugin alias, repository with optional release tag, or HTTPS artifact URL pinned with #sha256=<64 hex digits>.", Examples: []any{"shellcheck", "kjanat/actionlint-plugin-shellcheck@v0.2.0", "gitlab.com/group/plugin", "https://example.com/plugin.wasm#sha256=" + strings.Repeat("a", 64)}},
				mapping,
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
		for _, name := range ruleNames() {
			if name == "disallow-suppressions" {
				continue
			}
			rule.Examples = append(rule.Examples, name)
		}
		rule.Description = "Built-in rule ID or registered analyzer name. actionlint validates names against the loaded analyzers."
		rule.Not = &jsonschema.Schema{Enum: []any{"disallow-suppressions"}}
		for _, name := range []string{"file", "uses", "job", "step", "reason"} {
			property, _ := mapping.Properties.Get(name)
			// Match strings containing a character outside Go's Unicode whitespace set.
			property.Pattern = "[^\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]"
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
		return &jsonschema.Schema{
			Description: "Diagnostic level: default, off, on, info, warn, or error. Booleans enable or disable the rule.",
			AnyOf: []*jsonschema.Schema{
				{Type: "string", Enum: []any{"default", "off", "on", "info", "warn", "error"}},
				{Type: "boolean"},
				{Type: "null", Extras: map[string]any{"doNotSuggest": true}},
			},
		}
	case reflect.TypeFor[actionlint.Preset]():
		return &jsonschema.Schema{Description: "Baseline rule selection: recommended, all stable rules, or none.", Type: "string", Enum: []any{"recommended", "all", "none"}}
	case reflect.TypeFor[actionlint.LintRulesConfig]():
		mapping := reflectMapping(struct {
			Preset  actionlint.Preset `yaml:"preset" jsonschema:"description=Baseline selection: recommended, all stable rules, or none. Explicit rules override presets."`
			Disable []string          `yaml:"disable" jsonschema:"description=Suppress these diagnostic rule IDs at every severity. Omission or an empty list suppresses none."`
		}{})
		disable, _ := mapping.Properties.Get("disable")
		disable.Items.MinLength = new(uint64(1))
		for _, name := range ruleNames() {
			disable.Items.Examples = append(disable.Items.Examples, name)
		}
		disable.Type = ""
		disable.OneOf = []*jsonschema.Schema{{Type: "array"}, {Type: "null"}}
		for _, category := range []string{"correctness", "suspicious", "security", "policy", "external", "nursery", "plugins"} {
			group := reflectMapping(struct {
				Preset actionlint.Preset `yaml:"preset" jsonschema:"description=Baseline selection within this group: recommended, all, or none."`
				Level  actionlint.Level  `yaml:"level" jsonschema:"description=Default diagnostic level for rules in this group."`
			}{})
			group.Version, group.ID = "", ""
			for _, rule := range actionlint.Rules() {
				if rule.Group != category {
					continue
				}
				setting := reflectMapping(struct {
					Level actionlint.Level `yaml:"level" jsonschema:"required,description=Diagnostic level for this rule: default, off, on, info, warn, or error."`
				}{})
				setting.Version, setting.ID = "", ""
				setting.Properties.Set("options", &jsonschema.Schema{Type: "null", Description: "Reset rule options to their defaults."})
				if options := ruleOptions(rule.Name); options != nil {
					optionsSchema := reflectMapping(options)
					optionsSchema.Version, optionsSchema.ID = "", ""
					setting.Properties.Set("options", &jsonschema.Schema{Description: "Options specific to this rule; null resets to defaults.", AnyOf: []*jsonschema.Schema{{Type: "null"}, {AllOf: []*jsonschema.Schema{{Type: "object"}, optionsSchema}}}})
				}
				description := rule.Description
				if rule.Maturity == "nursery" {
					description += ". Nursery; disabled by default and excluded from stable presets."
				} else if !rule.Recommended {
					description += ". Stable; not enabled by the recommended preset."
				}
				level := reflectMapping(actionlint.Level(""))
				level.Description = ""
				group.Properties.Set(rule.Name, &jsonschema.Schema{Description: description, OneOf: []*jsonschema.Schema{level, setting}})
			}
			if category == "plugins" {
				setting := reflectMapping(struct {
					Level   actionlint.Level `yaml:"level" jsonschema:"required,description=Diagnostic level for the registered analyzer."`
					Options map[string]any   `yaml:"options,omitempty" jsonschema:"nullable,description=Options validated against the registered analyzer schema."`
				}{})
				setting.Version, setting.ID = "", ""
				group.AdditionalProperties = &jsonschema.Schema{OneOf: []*jsonschema.Schema{reflectMapping(actionlint.Level("")), setting}}
			}
			level := reflectMapping(actionlint.Level(""))
			level.Description = ""
			level.AnyOf[0].Enum = append(level.AnyOf[0].Enum, "all")
			description := "Rules concerning " + category + ". Use true/on to enable the group, false/off to disable it, all for its full preset, or a mapping for individual rules. Group settings override the root preset; individual settings override the group."
			if category == "nursery" {
				count := 0
				for _, rule := range actionlint.Rules() {
					if rule.Group == category {
						count++
					}
				}
				description = fmt.Sprintf("Experimental rules excluded from root presets. Currently contains %d rules. Select all to enable the nursery group explicitly.", count)
			}
			mapping.Properties.Set(category, &jsonschema.Schema{Description: description, OneOf: []*jsonschema.Schema{level, group}})
		}
		mapping.Version, mapping.ID = "", ""
		return mapping
	case reflect.TypeFor[actionlint.ShellcheckConfigSource]():
		markdown := "Inline directives or an rc file/directory.\n\nRelative paths use the configuration file's directory, or the analysis working directory when supplied by an overlay. `${{ configdir }}` explicitly selects the configuration directory; `${{ gitdir }}` selects the repository root.\n\n`${{ github.workspace }}` and `${{ github.action_path }}` resolve when their context is known."
		description := strings.ReplaceAll(markdown, "`", "")
		// Type-only alternatives keep unknown-key diagnostics attached to the mapping.
		return &jsonschema.Schema{
			Description: description,
			Extras:      map[string]any{"markdownDescription": markdown},
			AnyOf: []*jsonschema.Schema{
				{Type: "string", Extras: map[string]any{"defaultSnippets": []any{map[string]string{
					"label": "ShellCheck rc file or directory", "body": "${1:.shellcheckrc}",
				}}}},
				{Type: "object", Extras: map[string]any{"defaultSnippets": []any{map[string]string{
					"label": "Inline ShellCheck directives", "bodyText": "{}",
				}}}},
				{Type: "null"},
			},
			MinLength: new(uint64(1)),
			Not:       &jsonschema.Schema{Type: "string", Pattern: `[\x00\r\n]`},
			If:        &jsonschema.Schema{Type: "object"},
			Then: &jsonschema.Schema{AllOf: []*jsonschema.Schema{
				// Keep the selecting property's hover ahead of the referenced title.
				{Title: "config", Description: description, Extras: map[string]any{"markdownDescription": markdown}},
				{Ref: shellcheckSchemaPath},
			}},
		}
	case reflect.TypeFor[actionlint.ShellcheckToolConfig]():
		mapping := reflectMapping(struct {
			Backend *toolconfig.Backend                `yaml:"backend,omitempty" jsonschema:"description=Select the execution backend. Process settings apply only to the native adapter."`
			Enabled *bool                              `yaml:"enabled" jsonschema:"nullable,default=true,description=Enable ShellCheck analysis. Omission or null keeps it enabled."`
			Config  *actionlint.ShellcheckConfigSource `yaml:"config"`
		}{})
		mapping.Version, mapping.ID = "", ""
		config, _ := mapping.Properties.Get("config")
		config.Description = config.Then.AllOf[0].Description
		mapping.Type = ""
		mapping.OneOf = []*jsonschema.Schema{{Type: "boolean"}, {Type: "object"}, {Type: "null"}}
		requireOneBackend(mapping)
		return mapping
	case reflect.TypeFor[actionlint.RuffToolConfig]():
		type plain actionlint.RuffToolConfig
		mapping := reflectMapping(plain{})
		mapping.Version, mapping.ID = "", ""
		mapping.Type = ""
		mapping.OneOf = []*jsonschema.Schema{{Type: "boolean"}, {Type: "object"}, {Type: "null"}}
		mapping.Properties.Set("config", &jsonschema.Schema{
			Description: "Options validated by the selected Ruff engine. Omission or null uses engine defaults.",
			AnyOf:       []*jsonschema.Schema{{Type: "object"}, {Type: "null"}},
		})
		target, _ := mapping.Properties.Get("target-version")
		target.Type = ""
		target.OneOf = []*jsonschema.Schema{{Type: "string", MinLength: new(uint64(1)), Examples: []any{"py314"}}, {Type: "null", Extras: map[string]any{"doNotSuggest": true}}}
		for _, name := range []string{"select", "ignore"} {
			property, _ := mapping.Properties.Get(name)
			property.Items = &jsonschema.Schema{Type: "string", MinLength: new(uint64(1)), Examples: []any{"F", "F821"}}
		}
		requireOneBackend(mapping)
		for name, description := range map[string]string{
			"backend":        "Choose how Ruff runs. Process settings only apply to the native adapter.",
			"enabled":        "Enable Ruff analysis.",
			"target-version": "Python version to check against, such as `py312`. Leave this unset to use the version in the step's shell, or the selected engine's default if the shell doesn't specify one.",
			"select":         "Rule codes or prefixes to check, such as `F` or `F821`. Leave this unset to use Ruff's default selection.",
			"ignore":         "Rule codes or prefixes to skip, such as `F401`.",
		} {
			property, _ := mapping.Properties.Get(name)
			property.Description = strings.ReplaceAll(description, "`", "")
			if property.Extras == nil {
				property.Extras = map[string]any{}
			}
			property.Extras["markdownDescription"] = description
		}
		return mapping
	case reflect.TypeFor[actionlint.IgnorePatterns]():
		// JSON Schema's regex format uses a different dialect from Go's regexp.
		return &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}}
	case reflect.TypeFor[map[string]actionlint.IgnorePatterns]():
		return &jsonschema.Schema{
			Type: "object",
			AdditionalProperties: &jsonschema.Schema{OneOf: []*jsonschema.Schema{
				{Type: "array", Items: &jsonschema.Schema{Type: "string"}},
				{Type: "null"},
			}},
		}
	case reflect.TypeFor[actionlint.DefaultPermissionsAssumption]():
		return &jsonschema.Schema{
			Type:    "string",
			Enum:    []any{"restricted", "permissive"},
			Default: "restricted",
		}
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
		rules.Items.Description = "Built-in rule ID or registered analyzer name."
		rules.Items.MinLength = new(uint64(1))
		names := ruleNames()
		slices.Sort(names)
		for _, name := range names {
			rules.Items.Examples = append(rules.Items.Examples, name)
		}
		mapping.Version = ""
		mapping.ID = ""
		return &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "boolean"}, mapping}}
	default:
		return nil
	}
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
}

func requireOneBackend(mapping *jsonschema.Schema) {
	backend, _ := mapping.Properties.Get("backend")
	backend.OneOf = []*jsonschema.Schema{{Required: []string{"process"}}, {Required: []string{"wasm"}}}
}

func documentedReflector() (*jsonschema.Reflector, error) {
	r := reflector()
	if err := r.AddGoComments("actionlint.kjanat.dev/v2", "internal/config/rules/commit_hash.go", jsonschema.WithFullComment()); err != nil {
		return nil, err
	}
	if err := r.AddGoComments("actionlint.kjanat.dev/v2", "internal/config/tools/command.go", jsonschema.WithFullComment()); err != nil {
		return nil, err
	}
	for _, file := range []string{"config.go", "lint.go", "rule_settings.go", "catalog.go", "file_overrides.go", "knowledge_options.go"} {
		if err := r.AddGoComments("actionlint.kjanat.dev/v2", filepath.Join("internal", "config", file), jsonschema.WithFullComment()); err != nil {
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
	s.Properties.Set("$schema", &jsonschema.Schema{Type: "string", Description: "Schema reference for editors. actionlint ignores it."})
	// Resolve tool schemas beside this document in Git checkouts and npm packages.
	s.ID = ""
	s.Title = "actionlint configuration"
	s.Comments = "Generated from internal/config/config.go by go generate -run generate-config-schema. DO NOT EDIT."
	return encodeSchema(s)
}

func generateShellcheckSchema() ([]byte, error) {
	r, err := documentedReflector()
	if err != nil {
		return nil, err
	}
	s := r.Reflect(actionlint.ShellcheckConfig{})
	enable, _ := s.Properties.Get("enable")
	enable.OneOf = append(enable.OneOf, &jsonschema.Schema{Type: "string", Enum: []any{"all"}}, &jsonschema.Schema{Type: "boolean", Enum: []any{true}})
	enable.Description = enable.OneOf[0].Description
	enable.OneOf[0].Description = ""
	if description, ok := enable.OneOf[0].Extras["markdownDescription"]; ok {
		if enable.Extras == nil {
			enable.Extras = map[string]any{}
		}
		enable.Extras["markdownDescription"] = description
		delete(enable.OneOf[0].Extras, "markdownDescription")
	}
	sourcePath, _ := s.Properties.Get("source-path")
	// Match shellcheckDirectivePath: a directive cannot contain line breaks or
	// NUL. Both quote types require an unquoted path with no space/tab or leading quote.
	sourcePath.OneOf[0].Items.Not = &jsonschema.Schema{Pattern: `[\x00\r\n]`}
	sourcePath.OneOf[0].Items.AnyOf = []*jsonschema.Schema{
		{Not: &jsonschema.Schema{Pattern: `"`}},
		{Not: &jsonschema.Schema{Pattern: `'`}},
		{Not: &jsonschema.Schema{Pattern: `[ \t]|^['"]`}},
	}
	s.ID = ""
	s.Title = "ShellCheck " + shellcheckSchemaVersion + " inline directives for actionlint"
	s.Comments = "Versioned snapshot of actionlint's YAML representation of ShellCheck directives. Normal generation does not overwrite this file; see tools/config/README.md."
	return encodeSchema(s)
}

func initializeShellcheckSchema() error {
	b, err := generateShellcheckSchema()
	if err != nil {
		return err
	}
	return genfile.Write(context.Background(), shellcheckSchemaPath, b, true)
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-init-shellcheck-schema" {
		if err := initializeShellcheckSchema(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/config [-init-shellcheck-schema]")
		os.Exit(1)
	}
	b, err := generate()
	if err == nil {
		err = genfile.Write(context.Background(), "actionlint.schema.json", b, false)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func ruleNames() []string {
	rules := actionlint.Rules()
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, rule.Name)
	}
	return names
}

func ruleOptions(name string) any {
	if rule, ok := actionlint.FindRule(name); ok && rule.Options != nil {
		return rule.Options()
	}
	return nil
}
