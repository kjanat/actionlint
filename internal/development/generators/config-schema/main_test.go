package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	genfile "actionlint.kjanat.dev/v2/internal/development/generators/files"
	actionlint "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configschema "actionlint.kjanat.dev/v2/internal/input/configuration/schema"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"

	"github.com/google/go-cmp/cmp"
	"github.com/invopop/jsonschema"
	"github.com/kjanat/go-yaml/v4"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

const shellcheckSchemaPath = "schemas/shellcheck/0.11.0.schema.json"

func TestMetadataMirrorSchemaMatchesConfiguration(t *testing.T) {
	t.Parallel()
	var root any
	if err := json.Unmarshal(generatedSchema(t), &root); err != nil {
		t.Fatal(err)
	}
	const base = "file:///offline/actionlint.schema.json"
	compiler := validator.NewCompiler()
	compiler.UseLoader(validator.SchemeURLLoader{})
	addToolSchemas(t, compiler, base)
	if err := compiler.AddResource(base, root); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{`{}`, true},
		{`{"services":[]}`, true},
		{`{"services":["jsdelivr"]}`, true},
		{`{"services":null}`, true},
		{`{"allow-unknown-repositories":true}`, true},
		{`{"services":["typo"]}`, false},
		{`{"services":"jsdelivr"}`, false},
		{`{"allow-unknown-repositories":"true"}`, false},
		{`{"allow-unknown-repository":true}`, false},
	} {
		data := []byte(`{"network":{"metadata-mirrors":` + tc.input + `}}`)
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		_, parseErr := checking.ConfigurationRules().Parse(data)
		schemaErr := schema.Validate(value)
		if (parseErr == nil) != tc.valid || (schemaErr == nil) != tc.valid {
			t.Errorf("%s: want valid=%t, parser=%v, schema=%v", tc.input, tc.valid, parseErr, schemaErr)
		}
	}
}

func TestPermissionLocationsDoNotSuggestBothAsDefault(t *testing.T) {
	t.Parallel()
	schema := mapYAMLType(reflect.TypeFor[actionlint.PermissionLocationList](), nil)
	if schema.Default != nil {
		t.Fatalf("require-at has no value equivalent to omission; schema suggests %v", schema.Default)
	}
}

var (
	repositoryRoot   string
	generatedRoot    []byte
	errGeneratedRoot error
)

func TestMain(m *testing.M) {
	packageDirectory, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	repositoryRoot = filepath.Join(packageDirectory, "..", "..", "..", "..")
	if err := os.Chdir(repositoryRoot); err != nil {
		panic(err)
	}
	generatedRoot, errGeneratedRoot = generate()
	if err := os.Chdir(packageDirectory); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func generatedSchema(t *testing.T) []byte {
	t.Helper()
	if errGeneratedRoot != nil {
		t.Fatal(errGeneratedRoot)
	}
	return bytes.Clone(generatedRoot)
}

func TestTombiMetadata(t *testing.T) {
	t.Parallel()
	data := generatedSchema(t)
	var schema struct {
		Version string            `json:"x-tombi-toml-version"`
		Order   map[string]string `json:"x-tombi-table-keys-order"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Version != "v1.1.0" || schema.Order["properties"] != "schema" {
		t.Fatalf("unexpected Tombi metadata: %+v", schema)
	}
	// A multiline inline table with a trailing comma requires TOML 1.1.
	if _, err := document.Parse("actionlint.toml", []byte("lint = {\n  rules = { preset = 'recommended' },\n}\n")); err != nil {
		t.Fatalf("the parser rejects the TOML version advertised to Tombi: %v", err)
	}
	for _, label := range []string{"rule_name", "tool_name", "variable_name", "environment_name", "file_pattern"} {
		if !strings.Contains(string(data), `"x-tombi-additional-key-label": "`+label+`"`) {
			t.Errorf("missing completion label %q", label)
		}
	}
	if strings.Contains(string(data), "x-tombi-array-values-order") {
		t.Fatal("configuration arrays must not be reordered by the schema")
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			if value["deprecated"] == true && value["deprecationMessage"] != value["description"] {
				t.Errorf("deprecated schema entry has no replacement message: %v", value)
			}
			for _, child := range value {
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}
	visit(root)
}

func TestRuleLevelSuggestions(t *testing.T) {
	t.Parallel()
	if schema := mapYAMLType(reflect.TypeFor[actionlint.Level](), nil); schema.Ref != "#/$defs/level" {
		t.Fatalf("level property does not reference its shared definition: %+v", schema)
	}
	schema := levelSchema()
	if len(schema.OneOf) != 3 {
		t.Fatal("expected named levels, booleans, and null")
	}
	if diff := cmp.Diff([]any{"default", "off", "on", "info", "warn", "error"}, schema.OneOf[0].Enum); diff != "" {
		t.Fatal(diff)
	}
	if schema.OneOf[1].Type != "boolean" || schema.OneOf[1].Not != nil {
		t.Fatal("both boolean switches must be supported")
	}
	if schema.OneOf[2].Extras["doNotSuggest"] != true {
		t.Fatal("reset branch should not obscure level suggestions")
	}
}

func TestSchemaReferenceNotSuggested(t *testing.T) {
	t.Parallel()
	var schema struct {
		Properties        map[string]json.RawMessage `json:"properties"`
		PatternProperties map[string]struct {
			Type string `json:"type"`
		} `json:"patternProperties"`
	}
	if err := json.Unmarshal(generatedSchema(t), &schema); err != nil {
		t.Fatal(err)
	}
	if _, suggested := schema.Properties["$schema"]; suggested {
		t.Fatal("editor metadata must not be offered as a configuration setting")
	}
	if schema.PatternProperties[`^\$schema$`].Type != "string" {
		t.Fatal("an existing $schema string must remain accepted")
	}
}

func TestPluginRuleSelectors(t *testing.T) {
	t.Parallel()
	ignore := mapYAMLType(reflect.TypeFor[actionlint.IgnoreConfig](), nil)
	rule, _ := ignore.Properties.Get("rule")
	lint := mapYAMLType(reflect.TypeFor[actionlint.LintRulesConfig](), nil)
	disabled, _ := lint.Properties.Get("disable")
	policy := mapYAMLType(reflect.TypeFor[actionlint.SuppressionsPolicy](), nil)
	prohibited, _ := policy.OneOf[1].Properties.Get("rules")
	if len(rule.AllOf) != 1 {
		t.Fatal("ignore selectors must reference rule names and apply their suppression constraints")
	}
	for _, selector := range []*jsonschema.Schema{rule.AllOf[0], disabled.Items, prohibited.Items} {
		if selector.Ref != "#/$defs/rule-name" || len(selector.Examples) != 0 || len(selector.Enum) != 0 {
			t.Fatalf("rule selectors must reference shared names without copied catalogues: %+v", selector)
		}
	}
}

// ruffShorthand returns the generated schema of a key written directly under
// tools.ruff, as a JSON value.
func ruffShorthand(t *testing.T, key string) map[string]any {
	t.Helper()
	tools := mapYAMLType(reflect.TypeFor[actionlint.ToolsConfig](), nil)
	ruff, ok := tools.Properties.Get("ruff")
	if !ok {
		t.Fatal("missing Ruff tool schema")
	}
	property, ok := ruff.Properties.Get(key)
	if !ok {
		t.Fatalf("missing Ruff %s schema", key)
	}
	data, err := json.Marshal(property)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestRuffTargetVersionSuggestions(t *testing.T) {
	t.Parallel()
	var target struct {
		OneOf []struct {
			Type         string `json:"type"`
			Enum         []any  `json:"enum"`
			Ref          string `json:"$ref"`
			DoNotSuggest bool   `json:"doNotSuggest"`
		} `json:"oneOf"`
	}
	data, err := json.Marshal(ruffShorthand(t, "target-version"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &target); err != nil {
		t.Fatal(err)
	}
	if len(target.OneOf) != 2 || target.OneOf[1].Type != "null" || !target.OneOf[1].DoNotSuggest {
		t.Fatal("expected version choices and a hidden reset branch")
	}
	if target.OneOf[0].Type != "string" || len(target.OneOf[0].Enum) != 0 || target.OneOf[0].Ref != "" {
		t.Fatal("Ruff targets must be validated by the selected engine")
	}
}

func TestRuffSelectorSuggestions(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"select", "ignore"} {
		var property struct {
			Not   any `json:"not"`
			Items *struct {
				Ref       string `json:"$ref"`
				Enum      []any  `json:"enum"`
				Examples  []any  `json:"examples"`
				MinLength *int   `json:"minLength"`
			} `json:"items"`
		}
		data, err := json.Marshal(ruffShorthand(t, key))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &property); err != nil {
			t.Fatal(err)
		}
		if property.Items == nil {
			t.Fatalf("missing Ruff %s schema", key)
		}
		if property.Items.Ref != "" || len(property.Items.Enum) != 0 || !reflect.DeepEqual(property.Items.Examples, []any{"F", "F821"}) {
			t.Fatalf("Ruff %s completion must use small safe examples, not an upstream or copied catalogue", key)
		}
		if property.Not != nil || property.Items.MinLength == nil {
			t.Fatalf("Ruff %s must constrain shape without pinning engine semantics", key)
		}
	}
}

func TestRuffEditorCompletions(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed")
	}
	root := filepath.Join("..", "..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "node_modules", "yaml-language-server", "package.json")); err != nil {
		t.Skip("run bun install --frozen-lockfile first: yaml-language-server is not installed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	read := func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	}
	installed, err := configschema.Document(read, []manifests.Manifest{{Rule: rule.Descriptor{Name: "custom-check", Category: "conventions", Description: "A registered custom check"}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "installed.schema.json")
	if err := os.WriteFile(path, installed, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{{"editor-completions.mjs"}, {"editor-completions.mjs", path, "custom-check"}} {
		command := exec.CommandContext(ctx, node, arguments...)
		command.WaitDelay = time.Second
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("YAML language service regression: %v\n%s", err, output)
		}
		t.Log(string(output))
	}
}

func addToolSchemas(t *testing.T, compiler *validator.Compiler, base string) {
	t.Helper()
	rootURL, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{shellcheckSchemaPath} {
		data, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(filename)))
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		location := rootURL.ResolveReference(&url.URL{Path: filename}).String()
		if err := compiler.AddResource(location, document); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuffEngineIndependentSchema(t *testing.T) {
	t.Parallel()
	root := generatedSchema(t)
	var document any
	if err := json.Unmarshal(root, &document); err != nil {
		t.Fatal(err)
	}
	const base = "file:///offline/actionlint.schema.json"
	c := validator.NewCompiler()
	c.UseLoader(validator.SchemeURLLoader{})
	addToolSchemas(t, c, base)
	if err := c.AddResource(base, document); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile(base)
	if err != nil {
		t.Fatal(err)
	}
	selectors := []string{"F", "E111", "RUF055", "ENGINE999", ""}
	for _, key := range []string{"select", "ignore", "target-version"} {
		values := selectors
		if key == "target-version" {
			values = []string{"py36", "py316", "py399", ""}
		}
		for _, value := range values {
			for _, override := range []bool{false, true} {
				var property any = []any{value}
				want := value != ""
				if key == "target-version" {
					property = value
					want = value != ""
				}
				input := map[string]any{"tools": map[string]any{"ruff": map[string]any{key: property}}}
				if override {
					input["includes"] = []any{"**"}
					input = map[string]any{"overrides": []any{input}}
				}
				data, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				_, parseErr := checking.ConfigurationRules().Parse(data)
				schemaErr := schema.Validate(input)
				if (parseErr == nil) != want || (schemaErr == nil) != want {
					t.Fatalf("%s=%q override=%v want valid=%v: parser=%v schema=%v", key, value, override, want, parseErr, schemaErr)
				}
			}
		}
	}
}

func TestLintRuleCategoriesAcceptPluginsAndRejectWrongGroups(t *testing.T) {
	t.Parallel()
	root := generatedSchema(t)
	var document any
	if err := json.Unmarshal(root, &document); err != nil {
		t.Fatal(err)
	}
	const base = "file:///offline/actionlint.schema.json"
	compiler := validator.NewCompiler()
	compiler.UseLoader(validator.SchemeURLLoader{})
	addToolSchemas(t, compiler, base)
	if err := compiler.AddResource(base, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(base)
	if err != nil {
		t.Fatal(err)
	}
	validate := func(input string) error {
		var value any
		if err := json.Unmarshal([]byte(input), &value); err != nil {
			return err
		}
		return schema.Validate(value)
	}
	for _, input := range []string{
		`{"lint":{"rules":{"correctness":{"plugin-check":{"level":"warn","options":{"enabled":true}}}}}}`,
		`{"overrides":[{"includes":["a.yml"],"lint":{"rules":{"security":{"plugin-check":"off"}}}}]}`,
	} {
		if err := validate(input); err != nil {
			t.Errorf("%s rejected: %v", input, err)
		}
	}
	for _, input := range []string{
		`{"lint":{"rules":{"correctness":{"unpinned-reference":"off"}}}}`,
		`{"lint":{"rules":{"style":"all"}}}`,
		`{"overrides":[{"includes":["a.yml"],"lint":{"rules":{"external":"all"}}}]}`,
	} {
		if err := validate(input); err == nil {
			t.Errorf("%s accepted", input)
		}
	}
}

func TestGeneratedSchemaUpToDate(t *testing.T) {
	t.Parallel()
	got := generatedSchema(t)
	want, err := os.ReadFile(filepath.Join(repositoryRoot, "actionlint.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The schema formatter owns whitespace and object key ordering. Compare
	// JSON values here so unit tests do not need dprint installed.
	var gotDocument, wantDocument any
	if err := json.Unmarshal(got, &gotDocument); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantDocument); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(wantDocument, gotDocument); diff != "" {
		t.Fatalf("actionlint.schema.json is stale; run mise run generate:config-schema (-want +got):\n%s", diff)
	}
}

func TestGeneratedSchemaFormatting(t *testing.T) {
	t.Parallel()
	content := generatedSchema(t)
	if _, err := exec.LookPath("kprint"); err != nil {
		t.Skip("kprint is not installed")
	}
	want, err := os.ReadFile(filepath.Join(repositoryRoot, "actionlint.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "actionlint.schema.json")
	for _, exclusive := range []bool{true, false} {
		if err := genfile.Write(t.Context(), path, content, exclusive); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(string(want), string(got)); diff != "" {
			t.Fatalf("generated formatting differs (-want +got):\n%s", diff)
		}
		content = got
	}
}

func TestBundledProviderSchemas(t *testing.T) {
	t.Chdir("../../../..")
	providers, err := manifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		t.Run(provider.Rule.Name, func(t *testing.T) {
			t.Parallel()
			if len(provider.ConfigSchema) == 0 {
				return
			}
			var want map[string]any
			if err := json.Unmarshal(provider.ConfigSchema, &want); err != nil {
				t.Fatal(err)
			}
			settings, err := settingsSchema(provider.Rule.Name, want)
			if err != nil {
				t.Fatal(err)
			}
			var data []byte
			if settings.Ref != "" {
				data, err = os.ReadFile(settings.Ref)
			} else {
				data, err = json.Marshal(settings)
			}
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("generated settings differ from provider schema (-provider +generated):\n%s", diff)
			}
		})
	}
}

func TestProviderSettingsDoNotRequireHostFields(t *testing.T) {
	t.Chdir("../../../..")
	provider := manifests.Manifest{
		ConfigSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"new-check-option":{"type":"integer","minimum":7}},"required":["new-check-option"]}`),
	}
	provider.Rule.Name = "new-check"
	reflectMapping := func(value any) *jsonschema.Schema { return reflector().Reflect(value) }
	generated, err := toolSchema(provider, reflectMapping)
	if err != nil {
		t.Fatal(err)
	}
	generated.Definitions, err = sharedDefinitions(reflectMapping)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(generated)
	if err != nil {
		t.Fatal(err)
	}
	compiler := validator.NewCompiler()
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	const location = "https://example.test/new-check.schema.json"
	if err := compiler.AddResource(location, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		config string
		valid  bool
	}{
		{`{"config":{"new-check-option":7}}`, true},
		{`{"config":{"new-check-option":6}}`, false},
		{`{"config":{"new-check-option":"7"}}`, false},
		{`{"config":{}}`, false},
		{`{"config":{"new-check-option":7,"unknown":true}}`, false},
	} {
		t.Run(tc.config, func(t *testing.T) {
			t.Parallel()
			var value any
			if err := json.Unmarshal([]byte(tc.config), &value); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(value); (err == nil) != tc.valid {
				t.Fatalf("validation error = %v, want valid=%t", err, tc.valid)
			}
		})
	}
}

func TestShellcheckSchemaReference(t *testing.T) {
	t.Parallel()
	b := generatedSchema(t)
	if !strings.Contains(string(b), `"$ref": "`+shellcheckSchemaPath+`"`) {
		t.Fatal("root schema must reference the versioned ShellCheck schema")
	}
	if strings.Contains(string(b), `"source-path"`) {
		t.Fatal("root schema must not inline ShellCheck directive properties")
	}
}

func TestSchemaRelativeResolution(t *testing.T) {
	t.Parallel()
	root := generatedSchema(t)
	for _, base := range []string{
		"file:///offline/node_modules/@kjanat/actionlint/actionlint.schema.json",
		"https://cdn.jsdelivr.net/npm/@kjanat/actionlint@1.17.0/actionlint.schema.json",
		"https://raw.githubusercontent.com/kjanat/actionlint/b837c5abb3549967ff6f30c852ede599dabdb339/actionlint.schema.json",
	} {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			c := validator.NewCompiler()
			c.UseLoader(validator.SchemeURLLoader{})
			addToolSchemas(t, c, base)
			var document any
			if err := json.Unmarshal(root, &document); err != nil {
				t.Fatal(err)
			}
			if err := c.AddResource(base, document); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Compile(base); err != nil {
				t.Fatalf("schema must resolve its tool reference beside the loaded root without remote fallback: %v", err)
			}
		})
	}
}

func TestSchemaValidation(t *testing.T) {
	t.Parallel()
	b := generatedSchema(t)
	var document any
	if err := json.Unmarshal(b, &document); err != nil {
		t.Fatal(err)
	}
	c := validator.NewCompiler()
	// Resolve checked-in resources only. Schema tests must not access the network.
	c.UseLoader(validator.SchemeURLLoader{})
	const url = "https://example.com/actionlint.schema.json"
	addToolSchemas(t, c, url)
	if err := c.AddResource(url, document); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		input       string
		schemaValid bool
		parserValid bool
	}{
		{"empty", `{}`, true, true},
		{"schema reference", `{"$schema": "https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json"}`, true, true},
		{"invalid schema reference type", `{"$schema": 42}`, false, true},
		{"schema reference lookalike", `{"$schema-extra": "unexpected"}`, false, true},
		{"ruff wasm path", `tools: {ruff: {backend: {wasm: {path: ruff.wasm}}}}`, true, true},
		{"ruff empty wasm path", `tools: {ruff: {backend: {wasm: {path: ""}}}}`, false, false},
		{"shellcheck wasm path", `tools: {shellcheck: {backend: {wasm: {path: shellcheck.wasm}}}}`, true, true},
		{"shellcheck empty wasm path", `tools: {shellcheck: {backend: {wasm: {path: ""}}}}`, false, false},
		{"override empty wasm path", `overrides: [{includes: ['**'], tools: {ruff: {backend: {wasm: {path: ""}}}}}]`, false, false},
		{"runner knowledge", `repository: {runners: [gpu]}`, true, true},
		{"expression knowledge", `repository: {variables: {}, secrets: null}`, true, true},
		{"workflow knowledge", `repository: {default-workflow-permissions: write}`, true, true},
		{"unknown runner option", `lint: {rules: {correctness: {runner-label: {level: default, options: {label: [gpu]}}}}}`, false, false},
		{"message filters", `lint: {message-ignores: {'**': ['legacy.*']}}`, true, true},
		{"invalid message regex", `lint: {message-ignores: {'**': ['[']}}`, true, false},
		{"invalid message path", `lint: {message-ignores: {'[': []}}`, true, false},
		{"plugin shorthand", `plugins: [kjanat/actionlint-plugin-shellcheck@v0.2.0]`, true, true},
		{"plugin custom catalogue", `plugins: [{name: gitlab.com/group/plugin, version: v1, registry: 'https://example.com/catalog/'}]`, true, true},
		{"plugin pinned local candidate", `plugins: [{name: owner/repo, version: v1, registry: 'file:///C:/tmp/catalogue/', sha256: ` + strings.Repeat("a", 64) + `}]`, true, true},
		{"plugin candidate digest requires exact version", `plugins: [{name: owner/repo, registry: 'file:///C:/tmp/catalogue/', sha256: ` + strings.Repeat("a", 64) + `}]`, true, false},
		{"plugin candidate digest rejects aliases", `plugins: [{name: candidate, version: v1, registry: 'file:///C:/tmp/catalogue/', sha256: ` + strings.Repeat("a", 64) + `}]`, true, false},
		{"plugin candidate digest rejects published registry", `plugins: [{name: owner/repo, version: v1, registry: 'https://example.com/catalog/', sha256: ` + strings.Repeat("a", 64) + `}]`, true, false},
		{"plugin candidate digest requires local registry", `plugins: [{name: owner/repo, version: v1, sha256: ` + strings.Repeat("a", 64) + `}]`, true, false},
		{"plugin candidate digest shape", `plugins: [{name: owner/repo, version: v1, registry: 'file:///C:/tmp/catalogue/', sha256: bad}]`, false, false},
		{"plugin empty list", `plugins: []`, true, true},
		{"plugin null", `plugins: null`, true, true},
		{"plugin invalid entry", `plugins: [42]`, false, false},
		{"plugin null entry", `plugins: [null]`, false, false},
		{"plugin empty entry", `plugins: ['']`, false, false},
		{"plugin missing name", `plugins: [{version: v1}]`, false, false},
		{"plugin unknown field", `plugins: [{name: owner/repo, extra: true}]`, false, false},
		{"plugin direct source table", `plugins: [{source: 'https://example.org/plugin.wasm#sha256=` + strings.Repeat("a", 64) + `'}]`, true, true},
		{"plugin source without digest", `plugins: [{source: 'https://example.org/plugin.wasm'}]`, false, false},
		{"plugin source mixed with registry identity", `plugins: [{name: owner/repo, source: 'https://example.org/plugin.wasm#sha256=` + strings.Repeat("a", 64) + `'}]`, false, false},
		{"plugin source mixed with candidate digest", `plugins: [{source: 'https://example.org/plugin.wasm#sha256=` + strings.Repeat("a", 64) + `', sha256: ` + strings.Repeat("a", 64) + `}]`, false, false},
		{"plugin null version", `plugins: [{name: owner/repo, version: null}]`, false, false},
		{"plugin ignore requires registration", `ignores: [{rule: no-sudo, file: '**'}]`, true, false},
		{"ignore policy itself", `ignores: [{rule: disallow-suppressions, file: '**'}]`, false, false},
		{"ignore blank job", `ignores: [{rule: expression, job: ' '}]`, false, false},
		{"ignore blank step", `ignores: [{rule: expression, step: "\t\n"}]`, false, false},
		{"ignore blank file", `ignores: [{rule: expression, file: ' '}]`, false, false},
		{"ignore blank uses", `ignores: [{rule: expression, uses: ' '}]`, false, false},
		{"ignore unicode blank reason", `ignores: [{rule: expression, job: build, reason: "\u0085\u00a0\u3000"}]`, false, false},
		{"ignore padded reason", `ignores: [{rule: expression, job: build, reason: ' reviewed '}]`, true, true},
		{"ruff oldest target", `tools: {ruff: {target-version: py37}}`, true, true},
		{"ruff selector prefix", `tools: {ruff: {select: [F82]}}`, true, true},
		{"ruff selector alias", `tools: {ruff: {select: [C9, U004, SIM111]}}`, true, true},
		{"ruff preview rule codes", `tools: {ruff: {select: [E111, RUF055]}}`, true, true},
		{"ruff stable copyright rule", `tools: {ruff: {select: [CPY001]}}`, true, true},
		{"ruff removed rules", `tools: {ruff: {select: [ANN101, ANN102, S320]}}`, true, true},
		{"ruff redirected removed names", `tools: {ruff: {select: [PGH001, RUF035]}}`, true, true},
		{"ruff unknown selector", `tools: {ruff: {select: [XYZ]}}`, true, true},
		{"ruff unknown ignored selector", `tools: {ruff: {ignore: [F9999]}}`, true, true},
		{"ruff invalid selector override", `overrides: [{includes: ['**'], tools: {ruff: {select: [XYZ]}}}]`, true, true},
		{"ruff selection reset", `tools: {ruff: {select: null, ignore: null}}`, true, true},
		{"ruff empty selection", `tools: {ruff: {select: [], ignore: []}}`, true, true},
		{"ruff newest target", `tools: {ruff: {target-version: py315}}`, true, true},
		{"ruff omitted target", `tools: {ruff: {}}`, true, true},
		{"ruff target reset", `tools: {ruff: {target-version: null}}`, true, true},
		{"ruff empty single-quoted target", `tools: {ruff: {target-version: ''}}`, false, false},
		{"ruff empty double-quoted target", `tools: {ruff: {target-version: ""}}`, false, false},
		{"ruff unsupported old target", `tools: {ruff: {target-version: py30}}`, true, true},
		{"ruff unsupported future target", `tools: {ruff: {target-version: py316}}`, true, true},
		{"ruff override target", `overrides: [{includes: ['**'], tools: {ruff: {target-version: py315}}}]`, true, true},
		{"ruff unsupported override target", `overrides: [{includes: ['**'], tools: {ruff: {target-version: py36}}}]`, true, true},
		{"legacy nursery group accepted by parser but absent from editor schema", `lint: {rules: {nursery: {preset: all}}}`, false, true},
		{"legacy nursery level accepted by parser but absent from editor schema", `lint: {rules: {nursery: warn}}`, false, true},
		{"legacy nursery reset accepted by parser but absent from editor schema", `lint: {rules: {nursery: null}}`, false, true},
		{"stable rule not nursery", `lint: {rules: {nursery: {case-insensitive-conditions: on}}}`, false, false},
		{"old experimental group", `lint: {rules: {experimental: true}}`, false, false},
		{"extends file", `extends: [../shared.yml]`, true, false},
		{"extends empty", `extends: []`, true, true},
		{"extends invalid", `extends: base.yml`, false, false},
		{"file selection", `files: {includes: ['**/*.yml'], excludes: ['**/generated/**']}`, true, true},
		{"file selection empty", `files: {includes: []}`, true, true},
		{"file selection null", `files: {includes: null}`, true, true},
		{"file selection invalid", `files: {includes: [1]}`, false, false},
		{"file selection unknown", `files: {exclude: ['**']}`, false, false},
		{"file override exclusions", `overrides: [{includes: ['**'], excludes: ['**/generated/**']}]`, true, true},
		{"rule level", `lint: {rules: {correctness: {expression: warn}}}`, true, true},
		{"group level", `lint: {rules: {correctness: info}}`, true, true},
		{"group baseline", `lint: {rules: {correctness: {level: warn, expression: off}}}`, true, true},
		{"global preset", `lint: {rules: {preset: all}}`, true, true},
		{"legacy policy group preset accepted by parser but absent from editor schema", `lint: {rules: {policy: {preset: all}}}`, false, true},
		{"legacy policy group reset accepted by parser but absent from editor schema", `lint: {rules: {policy: null}}`, false, true},
		{"rule options", `lint: {rules: {conventions: {job-timeout: {level: error, options: {max-minutes: 30}}}}}`, true, true},
		{"removed required actions", `lint: {rules: {policy: {required-actions: {level: on, options: {actions: [actions/checkout]}}}}}`, false, false},
		{"removed empty required actions", `lint: {rules: {policy: {required-actions: {level: on, options: {actions: []}}}}}`, false, false},
		{"null rule options", `lint: {rules: {conventions: {job-timeout: {level: on, options: null}}}}`, true, true},
		{"rule null timeout bound", `lint: {rules: {conventions: {job-timeout: {level: on, options: {max-minutes: null}}}}}`, false, false},
		{"rule null minimum bound", `lint: {rules: {conventions: {job-timeout: {level: on, options: {min-minutes: null}}}}}`, false, false},
		{"permissions workflow location", `lint: {rules: {security: {missing-permissions: {level: error, options: {require-at: workflow}}}}}`, true, true},
		{"permissions job location", `lint: {rules: {security: {missing-permissions: {level: error, options: {require-at: every-job}}}}}`, true, true},
		{"permissions location array", `lint: {rules: {security: {missing-permissions: {level: error, options: {require-at: [workflow, every-job]}}}}}`, true, true},
		{"permissions empty locations", `lint: {rules: {security: {missing-permissions: {level: error, options: {require-at: []}}}}}`, false, false},
		{"permissions unknown location", `lint: {rules: {security: {missing-permissions: {level: error, options: {require-at: step}}}}}`, false, false},
		{"permissions any location", `lint: {rules: {security: {missing-permissions: {level: error, options: {require-at: any}}}}}`, false, false},
		{"rule null permissions location", `lint: {rules: {security: {missing-permissions: {level: on, options: {require-at: null}}}}}`, false, false},
		{"rule null suppression rules", `lint: {rules: {conventions: {forbidden-suppression: {level: on, options: {rules: null}}}}}`, false, false},
		{"rule null suppression report", `lint: {rules: {conventions: {forbidden-suppression: {level: on, options: {report: null}}}}}`, false, false},
		{"invalid group", `lint: {rules: {securty: {}}}`, false, false},
		{"invalid rule group", `lint: {rules: {policy: {case-insensitive-conditions: on}}}`, false, false},
		{"unknown plugin rule ID has shape-valid settings", `lint: {rules: {correctness: {typo: off}}}`, true, true},
		{"invalid rule level", `lint: {rules: {correctness: {expression: warning}}}`, false, false},
		{"invalid group level", `lint: {rules: {correctness: {level: warning}}}`, false, false},
		{"rule default", `lint: {rules: {correctness: {expression: null}}}`, true, true},
		{"named rule default", `lint: {rules: {correctness: {expression: default}}}`, true, true},
		{"named group default", `lint: {rules: {correctness: default}}`, true, true},
		{"named rule level default", `lint: {rules: {correctness: {expression: {level: default}}}}`, true, true},
		{"named group level default", `lint: {rules: {correctness: {level: default}}}`, true, true},
		{"rule disabled alias", `lint: {rules: {correctness: {expression: false}}}`, true, true},
		{"rule level disabled alias", `lint: {rules: {correctness: {expression: {level: false}}}}`, true, true},
		{"rule level default", `lint: {rules: {correctness: {expression: {level: null}}}}`, true, true},
		{"legacy policy reset accepted by parser but absent from editor schema", `lint: {rules: {policy: {require-job-timeout: {level: null, options: null}}}}`, false, true},
		{"group enabled alias", `lint: {rules: {security: true}}`, true, true},
		{"group all alias", `lint: {rules: {security: all}}`, true, true},
		{"rule enabled alias", `lint: {rules: {conventions: {forbidden-suppression: true}}}`, true, true},
		{"group disabled alias", `lint: {rules: {correctness: false}}`, true, true},
		{"group level disabled alias", `lint: {rules: {correctness: {level: false}}}`, true, true},
		{"group level default", `lint: {rules: {correctness: {level: null}}}`, true, true},
		{"invalid empty rule", `lint: {rules: {correctness: {expression: ''}}}`, false, false},
		{"invalid empty group", `lint: {rules: {correctness: ''}}`, false, false},
		{"invalid empty rule level", `lint: {rules: {correctness: {expression: {level: ''}}}}`, false, false},
		{"invalid empty group level", `lint: {rules: {correctness: {level: ''}}}`, false, false},
		{"invalid rule options", `lint: {rules: {correctness: {expression: {level: on, options: {}}}}}`, false, false},
		{"invalid options key", `lint: {rules: {conventions: {job-timeout: {level: on, options: {minutes: 30}}}}}`, false, false},
		{"invalid options type", `lint: {rules: {conventions: {job-timeout: {level: on, options: true}}}}`, false, false},
		{"missing rule level", `lint: {rules: {conventions: {job-timeout: {options: {max-minutes: 30}}}}}`, false, false},
		{"experimental typo", `lint: {rules: {experimental: {enable: [typo]}}}`, false, false},
		{"experimental unknown option", `lint: {rules: {experimental: {typo: true}}}`, false, false},
		{"experimental string boolean", `lint: {rules: {experimental: {enabled: 'true'}}}`, false, false},
		{"not policy", `policy: {mixed-type-matrix-filters: true}`, false, false},
		{"lint suppression", `lint: {rules: {disable: [expression, shellcheck]}}`, true, true},
		{"lint null suppression", `lint: {rules: {disable: null}}`, true, true},
		{"lint unknown rule requires registration", `lint: {rules: {disable: [typo]}}`, true, false},
		{"lint empty rule", `lint: {rules: {disable: ['']}}`, false, false},
		{"legacy external Ruff group accepted by parser but absent from editor schema", `lint: {rules: {external: {ruff: warn}}}`, false, true},
		{"plugin rule category accepts shape before registration binding", `lint: {rules: {correctness: {ruff: warn}}}`, true, true},
		{"legacy plugin group is not emitted or accepted", `lint: {rules: {plugins: {custom: {level: warn, options: {count: 2}}}}}`, false, true},
		{"lint unknown key", `lint: {rules: {typo: true}}`, false, false},
		{"file override", `overrides: [{includes: ['.github/workflows/**'], lint: {rules: {suspicious: on}}}]`, true, true},
		{"file override null lint", `overrides: [{includes: ['**'], lint: null}]`, true, true},
		{"file override tools", `overrides: [{includes: ['**'], tools: {shellcheck: {config: {disable: [SC2086]}}}}]`, true, true},
		{"file override tool shorthand", `overrides: [{includes: ['**'], tools: {shellcheck: false}}]`, true, true},
		{"file override null tools", `overrides: [{includes: ['**'], tools: null}]`, true, true},
		{"file override unknown tool requires registration", `overrides: [{includes: ['**'], tools: {typo: true}}]`, true, false},
		{"plugin tool requires registration", `tools: {my-tool: {enabled: true, config: {level: 2}}}`, true, false},
		{"plugin tool config path requires registration", `tools: {my-tool: {config: my-tool.toml}}`, true, false},
		{"plugin tool unknown key", `tools: {my-tool: {level: 2}}`, false, false},
		{"plugin tool invalid shorthand", `tools: {my-tool: 2}`, false, false},
		{"ruff flat and native target", `tools: {ruff: {target-version: py311, config: {target-version: py312}}}`, true, true},
		{"ruff flat and native selection", `tools: {ruff: {select: [E], config: {lint: {select: [F]}}}}`, true, true},
		{"ruff native top-level selection", `tools: {ruff: {config: {select: [E], lint: {select: [F]}}}}`, true, true},
		{"ruff flat selection with path config", `tools: {ruff: {select: [E], config: ruff.toml}}`, false, false},
		{"file override missing includes", `overrides: [{lint: {rules: {suspicious: on}}}]`, false, false},
		{"file override empty includes", `overrides: [{includes: []}]`, false, false},
		{"file override numeric include", `overrides: [{includes: [1]}]`, false, false},
		{"file override unknown key", `overrides: [{includes: ['**'], typo: true}]`, false, false},
		{"file override invalid glob", `overrides: [{includes: ['[']}]`, true, false},
		{"ShellCheck disabled", `tools: {shellcheck: {enabled: false}}`, true, true},
		{"ShellCheck shorthand enabled", `tools: {shellcheck: true}`, true, true},
		{"ShellCheck shorthand disabled", `tools: {shellcheck: false}`, true, true},
		{"ShellCheck shorthand null", `tools: {shellcheck: null}`, true, true},
		{"ShellCheck empty mapping", `tools: {shellcheck: {}}`, true, true},
		{"ShellCheck unknown option", `tools: {shellcheck: {typo: true}}`, false, false},
		{"ShellCheck empty directive mapping", `tools: {shellcheck: {config: {}}}`, true, true},
		{"ShellCheck rc path", `tools: {shellcheck: {config: ./.shellcheckrc}}`, true, true},
		{"ShellCheck rc directory", `tools: {shellcheck: {config: '${{ gitdir }}/.github/'}}`, true, true},
		{"ShellCheck rc config directory", `tools: {shellcheck: {config: '${{ configdir }}/.shellcheckrc'}}`, true, true},
		{"ShellCheck rc workspace directory", `tools: {shellcheck: {config: '${{ github.workspace }}/.shellcheckrc'}}`, true, true},
		{"ShellCheck rc action directory", `tools: {shellcheck: {config: '${{ github.action_path }}/.shellcheckrc'}}`, true, true},
		{"ShellCheck empty rc path", `tools: {shellcheck: {config: ''}}`, false, false},
		{"ShellCheck multiline rc path", `tools: {shellcheck: {config: "first\nsecond"}}`, false, false},
		{"ShellCheck trailing newline rc path", `tools: {shellcheck: {config: "file\n"}}`, false, false},
		{"ShellCheck carriage return rc path", `tools: {shellcheck: {config: "file\r"}}`, false, false},
		{"ShellCheck NUL rc path", `tools: {shellcheck: {config: "file\0"}}`, false, false},
		{"ShellCheck invalid rc type", `tools: {shellcheck: {config: false}}`, false, false},
		{"ShellCheck nullable", `tools: {shellcheck: {enabled: null, config: null}}`, true, true},
		{"ShellCheck directives", `tools: {shellcheck: {config: {disable: [SC2086, SC3000-SC4000, all], enable: [all], shell: bash, external-sources: false, extended-analysis: true, source-path: ['my scripts']}}}`, true, true},
		{"ShellCheck all optional", `tools: {shellcheck: {config: {enable: all}}}`, true, true},
		{"ShellCheck true optional", `tools: {shellcheck: {enabled: false, config: {enable: true}}}`, true, true},
		{"ShellCheck false optional", `tools: {shellcheck: {config: {enable: false}}}`, false, false},
		{"ShellCheck quoted true optional", `tools: {shellcheck: {config: {enable: 'true'}}}`, false, false},
		{"ShellCheck empty source path", `tools: {shellcheck: {config: {source-path: [""]}}}`, false, false},
		{"ShellCheck multiline source path", `tools: {shellcheck: {config: {source-path: ["first\nsecond"]}}}`, false, false},
		{"ShellCheck trailing newline source path", `tools: {shellcheck: {config: {source-path: ["file\n"]}}}`, false, false},
		{"ShellCheck carriage return source path", `tools: {shellcheck: {config: {source-path: ["file\r"]}}}`, false, false},
		{"ShellCheck NUL source path", `tools: {shellcheck: {config: {source-path: ["file\0"]}}}`, false, false},
		{"ShellCheck source path single quote", `tools: {shellcheck: {config: {source-path: ["user's scripts"]}}}`, true, true},
		{"ShellCheck source path double quote", `tools: {shellcheck: {config: {source-path: ['user"s scripts']}}}`, true, true},
		{"ShellCheck source path both quotes", `tools: {shellcheck: {config: {source-path: ["user's\"scripts"]}}}`, true, true},
		{"ShellCheck source path both quotes and hash", `tools: {shellcheck: {config: {source-path: ["dir'\"#part"]}}}`, true, true},
		{"ShellCheck source path both quotes and space", `tools: {shellcheck: {config: {source-path: ["user's\" scripts"]}}}`, false, false},
		{"ShellCheck source path both quotes and tab", `tools: {shellcheck: {config: {source-path: ["user's\"\tscripts"]}}}`, false, false},
		{"ShellCheck source path leading single quote", `tools: {shellcheck: {config: {source-path: ["'user\"scripts"]}}}`, false, false},
		{"ShellCheck source path leading double quote", `tools: {shellcheck: {config: {source-path: ["\"user'scripts"]}}}`, false, false},
		{"ShellCheck flag misplaced", `tools: {shellcheck: {config: {format: json}}}`, false, false},
		{"ShellCheck dialect", `tools: {shellcheck: {config: {shell: python}}}`, false, false},
		{"ShellCheck number code", `tools: {shellcheck: {config: {disable: [2086]}}}`, false, false},
		{"ShellCheck invalid code", `tools: {shellcheck: {config: {disable: [bad]}}}`, false, false},
		{"ShellCheck invalid bool", `tools: {shellcheck: {config: {extended-analysis: yes}}}`, false, false},
		{"ShellCheck unknown setting", `tools: {shellcheck: {config: {typo: []}}}`, false, false},
		{"all settings", `
self-hosted-runner:
  labels: [linux.2xlarge, custom-*]
config-variables: [DEFAULT_RUNNER]
config-secrets: [DEPLOY_TOKEN]
assume-default-permissions: permissive
paths:
  .github/workflows/**/*.yaml:
    ignore: ['(?i)shellcheck reported .+']
policy:
  require-commit-hash: true
  require-job-timeout: {min-minutes: 5, max-minutes: 60.5}
  require-permissions: {scope: job}
`, true, true},
		{"null settings", `
self-hosted-runner: null
config-variables: null
config-secrets: null
assume-default-permissions: null
paths: null
policy: null
`, true, true},
		{"null nested settings", `
self-hosted-runner: {labels: null}
paths: {'**': {ignore: null}}
policy: {require-commit-hash: null, require-job-timeout: null, require-permissions: null}
`, true, true},
		{"empty lists", `
self-hosted-runner: {labels: []}
config-variables: []
config-secrets: []
paths: {'**': {ignore: []}}
policy: {}
`, true, true},
		{"restricted permissions", `assume-default-permissions: restricted`, true, true},
		{"timeout enabled", `policy: {require-job-timeout: true}`, true, true},
		{"timeout disabled", `policy: {require-job-timeout: false}`, true, true},
		{"timeout without maximum", `policy: {require-job-timeout: {}}`, true, true},
		{"timeout minimum", `policy: {require-job-timeout: {min-minutes: 1.5}}`, true, true},
		{"timeout equal bounds", `policy: {require-job-timeout: {min-minutes: 5, max-minutes: 5}}`, true, true},
		{"timeout reversed bounds", `policy: {require-job-timeout: {min-minutes: 30, max-minutes: 5}}`, true, false},
		{"timeout zero minimum", `policy: {require-job-timeout: {min-minutes: 0}}`, false, false},
		{"timeout negative minimum", `policy: {require-job-timeout: {min-minutes: -1}}`, false, false},
		{"timeout null minimum", `policy: {require-job-timeout: {min-minutes: null}}`, false, false},
		{"permissions enabled", `policy: {require-permissions: true}`, true, true},
		{"permissions disabled", `policy: {require-permissions: false}`, true, true},
		{"permissions default scope", `policy: {require-permissions: {}}`, true, true},
		{"permissions workflow scope", `policy: {require-permissions: {scope: workflow}}`, true, true},
		{"permissions job scope", `policy: {require-permissions: {scope: job}}`, true, true},
		{"permissions unknown scope", `policy: {require-permissions: {scope: step}}`, false, false},
		{"permissions null scope", `policy: {require-permissions: {scope: null}}`, false, false},
		{"permissions unknown key", `policy: {require-permissions: {level: job}}`, false, false},
		{"permissions string", `policy: {require-permissions: 'true'}`, false, false},
		{"commit hash disabled", `policy: {require-commit-hash: false}`, true, true},
		{"cache policies enabled", `policy: {cache-call-unrestricted: true, cache-operation: true, cache-write-untrusted: true}`, true, true},
		{"cache policies disabled", `policy: {cache-call-unrestricted: false, cache-operation: false, cache-write-untrusted: false}`, true, true},
		{"cache policies default", `policy: {cache-call-unrestricted: null, cache-operation: null, cache-write-untrusted: null}`, true, true},
		{"cache call policy string", `policy: {cache-call-unrestricted: 'false'}`, false, false},
		{"cache operation policy number", `policy: {cache-operation: 1}`, false, false},
		{"cache write policy mapping", `policy: {cache-write-untrusted: {}}`, false, false},
		{"suppressions forbidden", `policy: {disallow-suppressions: true}`, true, true},
		{"suppressions allowed", `policy: {disallow-suppressions: false}`, true, true},
		{"suppressions default", `policy: {disallow-suppressions: null}`, true, true},
		{"suppressions empty mapping", `policy: {disallow-suppressions: {}}`, true, true},
		{"suppressions selected", `policy: {disallow-suppressions: {rules: [cache-operation], report: all}}`, true, true},
		{"suppressions directive only", `policy: {disallow-suppressions: {report: suppression}}`, true, true},
		{"suppressions violation only", `policy: {disallow-suppressions: {report: violation}}`, true, true},
		{"suppressions duplicate selector", `policy: {disallow-suppressions: {rules: [cache-operation, cache-operation]}}`, true, true},
		{"suppressions string", `policy: {disallow-suppressions: 'true'}`, false, false},
		{"suppressions sequence", `policy: {disallow-suppressions: []}`, false, false},
		{"suppressions empty rules", `policy: {disallow-suppressions: {rules: []}}`, false, false},
		{"suppressions null rules", `policy: {disallow-suppressions: {rules: null}}`, false, false},
		{"suppressions scalar rules", `policy: {disallow-suppressions: {rules: cache-operation}}`, false, false},
		{"suppressions unregistered rule", `policy: {disallow-suppressions: {rules: [cache-operation, typo]}}`, true, false},
		{"suppressions empty rule", `policy: {disallow-suppressions: {rules: ['']}}`, false, false},
		{"ignore unregistered rule", `ignores: [{rule: no-sudo, job: check}]`, true, false},
		{"suppressions numeric rule", `policy: {disallow-suppressions: {rules: [1]}}`, false, false},
		{"suppressions unknown report", `policy: {disallow-suppressions: {report: ignore}}`, false, false},
		{"suppressions null report", `policy: {disallow-suppressions: {report: null}}`, false, false},
		{"suppressions unknown field", `policy: {disallow-suppressions: {unknown: true}}`, false, false},
		{"unknown setting", `config-secret: []`, false, true},
		{"unknown runner setting", `self-hosted-runner: {label: []}`, false, true},
		{"unknown path setting", `paths: {'**': {ignores: []}}`, false, true},
		{"unknown policy", `policy: {require-hash: true}`, false, false},
		{"unknown timeout setting", `policy: {require-job-timeout: {minutes: 60}}`, false, false},
		{"unknown permissions", `assume-default-permissions: write`, false, false},
		{"numeric permissions", `assume-default-permissions: 1`, false, false},
		{"scalar secrets", `config-secrets: TOKEN`, false, false},
		{"scalar regex", `paths: {'**': {ignore: 'foo'}}`, false, false},
		{"non-string regex", `paths: {'**': {ignore: [true]}}`, false, true},
		{"unclosed regex class", `paths: {'**': {ignore: ['[']}}`, true, false},
		{"unsupported regex lookahead", `paths: {'**': {ignore: ['(?=foo)']}}`, true, false},
		{"unclosed path class", `paths: {'[': {ignore: []}}`, true, false},
		{"unclosed path alternatives", `paths: {'{foo,bar': {ignore: []}}`, true, false},
		{"non-boolean policy", `policy: {require-commit-hash: 1}`, false, false},
		{"removed legacy required actions", `policy: {required-actions: [actions/checkout]}`, false, false},
		{"removed empty legacy required actions", `policy: {required-actions: []}`, false, false},
		{"timeout string", `policy: {require-job-timeout: 'true'}`, false, false},
		{"timeout zero", `policy: {require-job-timeout: {max-minutes: 0}}`, false, false},
		{"timeout negative", `policy: {require-job-timeout: {max-minutes: -1}}`, false, false},
		{"timeout null maximum", `policy: {require-job-timeout: {max-minutes: null}}`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var value any
			if err := yaml.Unmarshal([]byte(tt.input), &value); err != nil {
				t.Fatal(err)
			}
			// Legacy syntax must be migrated before schema validation.
			legacy := false
			if root, ok := value.(map[string]any); ok {
				for _, key := range []string{"policy", "self-hosted-runner", "config-variables", "config-secrets", "assume-default-permissions", "paths"} {
					_, exists := root[key]
					legacy = legacy || exists
				}
			}
			schemaValid := tt.schemaValid && !legacy
			if err := schema.Validate(value); (err == nil) != schemaValid {
				t.Errorf("schema validation error = %v, want valid = %v", err, schemaValid)
			}
			if legacy && tt.schemaValid && tt.parserValid {
				migration, err := checking.ConfigurationRules().MigrateDocument("config.yaml", []byte(tt.input))
				if err != nil {
					t.Fatal(err)
				}
				var converted any
				if err := yaml.Unmarshal(migration.Content, &converted); err != nil {
					t.Fatal(err)
				}
				if err := schema.Validate(converted); err != nil {
					t.Fatalf("migrated document violates current schema: %v\n%s", err, migration.Content)
				}
			}
			if _, err := checking.ConfigurationRules().Parse([]byte(tt.input)); (err == nil) != tt.parserValid {
				t.Errorf("parser validation error = %v, want valid = %v", err, tt.parserValid)
			}
		})
	}
}

func TestDocumentedReflectorUsesImportPaths(t *testing.T) {
	t.Chdir("../../../..")
	r, err := documentedReflector()
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeFor[actionlint.PathConfig]()
	key := typ.PkgPath() + "." + typ.Name() + ".Ignore"
	if !strings.HasPrefix(r.CommentMap[key], "Ignore suppresses diagnostics") {
		t.Fatalf("comment missing under Go import path %q", key)
	}
	if !strings.HasPrefix(r.LookupComment(typ, "Ignore"), "Suppresses diagnostics") {
		t.Fatal("field hover did not retain its source documentation")
	}
	for key := range r.CommentMap {
		if strings.Contains(key, `\`) {
			t.Errorf("comment key contains a filesystem separator: %q", key)
		}
	}
}

func TestSchemaDescriptions(t *testing.T) {
	t.Parallel()
	b := generatedSchema(t)
	var document map[string]any
	if err := json.Unmarshal(b, &document); err != nil {
		t.Fatal(err)
	}
	// Hover must work on the property itself, even when the value is null.
	// Finding a description somewhere inside a non-null branch is not enough.
	var check func(any, string)
	check = func(value any, path string) {
		switch value := value.(type) {
		case map[string]any:
			if properties, ok := value["properties"].(map[string]any); ok {
				for name, raw := range properties {
					property := raw.(map[string]any)
					t.Run(path+"."+name, func(t *testing.T) {
						t.Parallel()
						if property["title"] != name {
							t.Errorf("hover title = %v, want %s", property["title"], name)
						}
						description, _ := property["description"].(string)
						if description == "" {
							t.Fatal("property has no description outside its type variants")
						}
						markdown, _ := property["markdownDescription"].(string)
						if strings.ReplaceAll(markdown, "`", "") != strings.ReplaceAll(description, "`", "") {
							t.Error("hover is missing the Markdown version of the field documentation")
						}
					})
				}
			}
			for key, child := range value {
				check(child, path+"."+key)
			}
		case []any:
			for _, child := range value {
				check(child, path)
			}
		}
	}
	check(document, "config")
	toolSchema, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(shellcheckSchemaPath)))
	if err != nil {
		t.Fatal(err)
	}
	var toolDocument map[string]any
	if err := json.Unmarshal(toolSchema, &toolDocument); err != nil {
		t.Fatal(err)
	}
	check(toolDocument, "shellcheck")
}
