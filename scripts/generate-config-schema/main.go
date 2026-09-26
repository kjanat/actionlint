// Generate an editor schema from the configuration types and their Go comments.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"actionlint.kjanat.dev"
	"github.com/invopop/jsonschema"
)

const shellcheckSchemaVersion = "0.11.0"
const shellcheckSchemaPath = "schemas/shellcheck/" + shellcheckSchemaVersion + ".schema.json"

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
		return r.Reflect(value)
	}
	switch t {
	case reflect.TypeFor[actionlint.ShellcheckConfigSource]():
		markdown := "Inline directives or an rc file/directory.\n\nRelative paths use the configuration file's directory, or the analysis working directory when supplied by an overlay. `${{ configdir }}` explicitly selects the configuration directory; `${{ gitdir }}` selects the repository root.\n\n`${{ github.workspace }}` and `${{ github.action_path }}` resolve when their context is known."
		description := strings.ReplaceAll(markdown, "`", "")
		// Select alternatives by type alone: editors can then report unknown
		// mapping keys instead of falling back to an unrelated scalar error.
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
			Enabled *bool                              `yaml:"enabled" jsonschema:"nullable,default=true,description=Enable ShellCheck analysis. Omission or null keeps it enabled."`
			Config  *actionlint.ShellcheckConfigSource `yaml:"config"`
		}{})
		mapping.Version, mapping.ID = "", ""
		config, _ := mapping.Properties.Get("config")
		config.Description = config.Then.AllOf[0].Description
		mapping.Type = ""
		mapping.OneOf = []*jsonschema.Schema{{Type: "boolean"}, {Type: "object"}, {Type: "null"}}
		return mapping
	case reflect.TypeFor[actionlint.IgnorePatterns]():
		// JSON Schema's regex format uses a different dialect from Go's regexp.
		return &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}}
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
			Report string   `yaml:"report" jsonschema:"enum=suppression,enum=violation,enum=all,default=all,description=Report the prohibited directive or retain original violations or both."`
		}{})
		rules, _ := mapping.Properties.Get("rules")
		names := actionlint.InlineSuppressibleRules()
		slices.Sort(names)
		for _, name := range names {
			rules.Items.Enum = append(rules.Items.Enum, name)
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

func documentedReflector() (*jsonschema.Reflector, error) {
	r := reflector()
	if err := r.AddGoComments("actionlint.kjanat.dev", "config.go", jsonschema.WithFullComment()); err != nil {
		return nil, fmt.Errorf("read config comments: %w", err)
	}
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
	// Resolve tool schemas beside this document in Git checkouts and npm packages.
	s.ID = ""
	s.Title = "actionlint configuration"
	s.Comments = "Generated from config.go by go generate -run generate-config-schema. DO NOT EDIT."
	return encodeSchema(s)
}

func generateShellcheckSchema() ([]byte, error) {
	r, err := documentedReflector()
	if err != nil {
		return nil, err
	}
	s := r.Reflect(actionlint.ShellcheckConfig{})
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
	s.Comments = "Versioned snapshot of actionlint's YAML representation of ShellCheck directives. Normal generation does not overwrite this file; see scripts/generate-config-schema/README.md."
	return encodeSchema(s)
}

func initializeShellcheckSchema() error {
	b, err := generateShellcheckSchema()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(shellcheckSchemaPath), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(shellcheckSchemaPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
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
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/generate-config-schema [-init-shellcheck-schema]")
		os.Exit(1)
	}
	b, err := generate()
	if err == nil {
		err = os.WriteFile("actionlint.schema.json", b, 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
