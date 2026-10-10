package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
	"actionlint.kjanat.dev/internal/ruff"
	"github.com/google/go-cmp/cmp"
	"github.com/invopop/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v4"
)

func generatedSchema(t *testing.T) []byte {
	t.Helper()
	t.Chdir("../..")
	b, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRuleLevelSuggestions(t *testing.T) {
	schema := mapYAMLType(reflect.TypeFor[actionlint.RuleLevel](), nil)
	if len(schema.AnyOf) != 3 {
		t.Fatal("expected named levels and two hidden compatibility branches")
	}
	if diff := cmp.Diff([]any{"default", "off", "on", "info", "warn", "error"}, schema.AnyOf[0].Enum); diff != "" {
		t.Fatalf("suggest only named levels (-want +got):\n%s", diff)
	}
	for _, branch := range schema.AnyOf[1:] {
		if branch.Extras["doNotSuggest"] != true || len(branch.Enum) != 0 || branch.Description != "" {
			t.Fatal("compatibility values must not be suggested or advertised")
		}
	}
	if strings.Contains(schema.Description, "false") || strings.Contains(schema.Description, "null") {
		t.Fatal("describe only named levels")
	}
}

func TestRuffTargetVersionSuggestions(t *testing.T) {
	schema := mapYAMLType(reflect.TypeFor[actionlint.RuffToolConfig](), nil)
	target, ok := schema.Properties.Get("target-version")
	if !ok {
		t.Fatal("missing Ruff target-version schema")
	}
	want := []any{"py37", "py38", "py39", "py310", "py311", "py312", "py313", "py314", "py315"}
	if len(target.OneOf) != 2 || target.OneOf[1].Type != "null" || target.OneOf[1].Extras["doNotSuggest"] != true {
		t.Fatal("expected version choices and a hidden reset branch")
	}
	if diff := cmp.Diff(want, target.OneOf[0].Enum); diff != "" {
		t.Fatalf("Ruff target choices (-want +got):\n%s", diff)
	}
}

func TestRuffSelectorSuggestions(t *testing.T) {
	schema := mapYAMLType(reflect.TypeFor[actionlint.RuffToolConfig](), nil)
	var root jsonschema.Schema
	generated := generatedSchema(t)
	if strings.Count(string(generated), `"#/$defs/RuffRuleSelector"`) != 4 {
		t.Fatal("base and override selections must share the selector definition")
	}
	if err := json.Unmarshal(generated, &root); err != nil {
		t.Fatal(err)
	}
	var want []any
	for _, selector := range ruff.SupportedRuleSelectors() {
		want = append(want, selector)
	}
	for _, key := range []string{"select", "ignore"} {
		property, ok := schema.Properties.Get(key)
		if !ok || property.Items == nil {
			t.Fatalf("missing Ruff %s schema", key)
		}
		if property.Items.Ref != "#/$defs/RuffRuleSelector" {
			t.Fatalf("Ruff %s must reference the shared selector definition", key)
		}
		definition := root.Definitions[strings.TrimPrefix(property.Items.Ref, "#/$defs/")]
		if definition == nil || definition.Type != "string" {
			t.Fatal("missing shared Ruff selector definition")
		}
		if diff := cmp.Diff(want, definition.Enum); diff != "" {
			t.Fatalf("Ruff %s selectors (-want +got):\n%s", key, diff)
		}
	}
}

func TestGeneratedSchemaUpToDate(t *testing.T) {
	got := generatedSchema(t)
	want, err := os.ReadFile("actionlint.schema.json")
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
		t.Fatalf("actionlint.schema.json is stale; run go generate -run generate-config-schema (-want +got):\n%s", diff)
	}
}

func TestShellcheckSchemaSnapshot(t *testing.T) {
	t.Chdir("../..")
	generated, err := generateShellcheckSchema()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := os.ReadFile(shellcheckSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(generated, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(snapshot, &want); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ShellCheck schema contract changed; add a versioned snapshot for new directives, or explicitly review corrections to the existing version. See README.md (-snapshot +current):\n%s", diff)
	}
	if err := initializeShellcheckSchema(); !errors.Is(err, os.ErrExist) {
		t.Fatalf("initialization must refuse to overwrite an existing version, got %v", err)
	}
}

func TestShellcheckSchemaReference(t *testing.T) {
	b := generatedSchema(t)
	if !strings.Contains(string(b), `"$ref": "`+shellcheckSchemaPath+`"`) {
		t.Fatal("root schema must reference the versioned ShellCheck schema")
	}
	if strings.Contains(string(b), `"source-path"`) {
		t.Fatal("root schema must not inline ShellCheck directive properties")
	}
}

func TestSchemaRelativeResolution(t *testing.T) {
	root := generatedSchema(t)
	tool, err := os.ReadFile(shellcheckSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{
		"file:///offline/node_modules/@kjanat/actionlint/actionlint.schema.json",
		"https://cdn.jsdelivr.net/npm/@kjanat/actionlint@1.17.0/actionlint.schema.json",
		"https://raw.githubusercontent.com/kjanat/actionlint/b837c5abb3549967ff6f30c852ede599dabdb339/actionlint.schema.json",
	} {
		t.Run(base, func(t *testing.T) {
			rootURL, err := url.Parse(base)
			if err != nil {
				t.Fatal(err)
			}
			toolURL := rootURL.ResolveReference(&url.URL{Path: shellcheckSchemaPath}).String()
			c := validator.NewCompiler()
			c.UseLoader(validator.SchemeURLLoader{})
			for location, data := range map[string][]byte{base: root, toolURL: tool} {
				var document any
				if err := json.Unmarshal(data, &document); err != nil {
					t.Fatal(err)
				}
				if err := c.AddResource(location, document); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.Compile(base); err != nil {
				t.Fatalf("schema must resolve its tool reference beside the loaded root without remote fallback: %v", err)
			}
		})
	}
}

func TestSchemaValidation(t *testing.T) {
	b := generatedSchema(t)
	var document any
	if err := json.Unmarshal(b, &document); err != nil {
		t.Fatal(err)
	}
	c := validator.NewCompiler()
	// Resolve checked-in resources only. Schema tests must not access the network.
	c.UseLoader(validator.SchemeURLLoader{})
	toolSchema, err := os.ReadFile(shellcheckSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var toolDocument any
	if err := json.Unmarshal(toolSchema, &toolDocument); err != nil {
		t.Fatal(err)
	}
	if err := c.AddResource("https://example.com/"+shellcheckSchemaPath, toolDocument); err != nil {
		t.Fatal(err)
	}
	const url = "https://example.com/actionlint.schema.json"
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
		{"ruff oldest target", `tools: {ruff: {target-version: py37}}`, true, true},
		{"ruff selector prefix", `tools: {ruff: {select: [F82]}}`, true, true},
		{"ruff selector alias", `tools: {ruff: {select: [C9, U004, SIM111]}}`, true, true},
		{"ruff preview rule codes", `tools: {ruff: {select: [E111, RUF055]}}`, false, false},
		{"ruff stable copyright rule", `tools: {ruff: {select: [CPY001]}}`, true, true},
		{"ruff removed rules", `tools: {ruff: {select: [ANN101, ANN102, S320]}}`, false, false},
		{"ruff redirected removed names", `tools: {ruff: {select: [PGH001, RUF035]}}`, true, true},
		{"ruff unknown selector", `tools: {ruff: {select: [XYZ]}}`, false, false},
		{"ruff unknown ignored selector", `tools: {ruff: {ignore: [F9999]}}`, false, false},
		{"ruff invalid selector override", `overrides: [{includes: ['**'], tools: {ruff: {select: [XYZ]}}}]`, false, false},
		{"ruff selection reset", `tools: {ruff: {select: null, ignore: null}}`, true, true},
		{"ruff empty selection", `tools: {ruff: {select: [], ignore: []}}`, true, true},
		{"ruff newest target", `tools: {ruff: {target-version: py315}}`, true, true},
		{"ruff omitted target", `tools: {ruff: {}}`, true, true},
		{"ruff target reset", `tools: {ruff: {target-version: null}}`, true, true},
		{"ruff empty single-quoted target", `tools: {ruff: {target-version: ''}}`, false, false},
		{"ruff empty double-quoted target", `tools: {ruff: {target-version: ""}}`, false, false},
		{"ruff unsupported old target", `tools: {ruff: {target-version: py30}}`, false, false},
		{"ruff unsupported future target", `tools: {ruff: {target-version: py316}}`, false, false},
		{"ruff override target", `overrides: [{includes: ['**'], tools: {ruff: {target-version: py315}}}]`, true, true},
		{"ruff unsupported override target", `overrides: [{includes: ['**'], tools: {ruff: {target-version: py36}}}]`, false, false},
		{"nursery group", `lint: {rules: {nursery: {preset: all}}}`, true, true},
		{"nursery level", `lint: {rules: {nursery: warn}}`, true, true},
		{"nursery null", `lint: {rules: {nursery: null}}`, true, true},
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
		{"group preset", `lint: {rules: {policy: {preset: all}}}`, true, true},
		{"group null", `lint: {rules: {policy: null}}`, true, true},
		{"rule options", `lint: {rules: {policy: {require-job-timeout: {level: error, options: {max-minutes: 30}}}}}`, true, true},
		{"rule actions", `lint: {rules: {policy: {required-actions: {level: warn, options: {actions: ['actions/checkout']}}}}}`, true, true},
		{"rule empty actions", `lint: {rules: {policy: {required-actions: {level: on, options: {actions: []}}}}}`, true, true},
		{"rule null actions", `lint: {rules: {policy: {required-actions: {level: on, options: {actions: null}}}}}`, false, false},
		{"rule omitted actions", `lint: {rules: {policy: {required-actions: {level: on, options: {}}}}}`, false, false},
		{"rule null options", `lint: {rules: {policy: {required-actions: {level: on, options: null}}}}`, false, false},
		{"rule null timeout bound", `lint: {rules: {policy: {require-job-timeout: {level: on, options: {max-minutes: null}}}}}`, false, false},
		{"rule null minimum bound", `lint: {rules: {policy: {require-job-timeout: {level: on, options: {min-minutes: null}}}}}`, false, false},
		{"rule null permissions scope", `lint: {rules: {policy: {require-permissions: {level: on, options: {scope: null}}}}}`, false, false},
		{"rule null suppression rules", `lint: {rules: {policy: {disallow-suppressions: {level: on, options: {rules: null}}}}}`, false, false},
		{"rule null suppression report", `lint: {rules: {policy: {disallow-suppressions: {level: on, options: {report: null}}}}}`, false, false},
		{"invalid group", `lint: {rules: {securty: {}}}`, false, false},
		{"invalid rule group", `lint: {rules: {policy: {case-insensitive-conditions: on}}}`, false, false},
		{"invalid rule name", `lint: {rules: {correctness: {typo: off}}}`, false, false},
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
		{"group disabled alias", `lint: {rules: {correctness: false}}`, true, true},
		{"group level disabled alias", `lint: {rules: {correctness: {level: false}}}`, true, true},
		{"group level default", `lint: {rules: {correctness: {level: null}}}`, true, true},
		{"invalid empty rule", `lint: {rules: {correctness: {expression: ''}}}`, false, false},
		{"invalid empty group", `lint: {rules: {correctness: ''}}`, false, false},
		{"invalid empty rule level", `lint: {rules: {correctness: {expression: {level: ''}}}}`, false, false},
		{"invalid empty group level", `lint: {rules: {correctness: {level: ''}}}`, false, false},
		{"invalid rule options", `lint: {rules: {correctness: {expression: {level: on, options: {}}}}}`, false, false},
		{"invalid options key", `lint: {rules: {policy: {require-job-timeout: {level: on, options: {minutes: 30}}}}}`, false, false},
		{"invalid options type", `lint: {rules: {policy: {require-job-timeout: {level: on, options: true}}}}`, false, false},
		{"missing rule level", `lint: {rules: {policy: {require-job-timeout: {options: {max-minutes: 30}}}}}`, false, false},
		{"experimental typo", `lint: {rules: {experimental: {enable: [typo]}}}`, false, false},
		{"experimental unknown option", `lint: {rules: {experimental: {typo: true}}}`, false, false},
		{"experimental string boolean", `lint: {rules: {experimental: {enabled: 'true'}}}`, false, false},
		{"not policy", `policy: {mixed-type-matrix-filters: true}`, false, false},
		{"lint suppression", `lint: {rules: {disable: [expression, shellcheck]}}`, true, true},
		{"lint null suppression", `lint: {rules: {disable: null}}`, true, true},
		{"lint unknown rule", `lint: {rules: {disable: [typo]}}`, false, false},
		{"lint unknown key", `lint: {rules: {typo: true}}`, false, false},
		{"file override", `overrides: [{includes: ['.github/workflows/**'], lint: {rules: {suspicious: on}}}]`, true, true},
		{"file override null lint", `overrides: [{includes: ['**'], lint: null}]`, true, true},
		{"file override tools", `overrides: [{includes: ['**'], tools: {shellcheck: {config: {disable: [SC2086]}}}}]`, true, true},
		{"file override tool shorthand", `overrides: [{includes: ['**'], tools: {shellcheck: false}}]`, true, true},
		{"file override null tools", `overrides: [{includes: ['**'], tools: null}]`, true, true},
		{"file override unknown tool", `overrides: [{includes: ['**'], tools: {typo: true}}]`, false, false},
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
  required-actions: [actions/checkout, 'github/codeql-action/*@v4*']
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
policy: {require-commit-hash: null, require-job-timeout: null, require-permissions: null, required-actions: null}
`, true, true},
		{"empty lists", `
self-hosted-runner: {labels: []}
config-variables: []
config-secrets: []
paths: {'**': {ignore: []}}
policy: {required-actions: []}
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
		{"suppressions unknown rule", `policy: {disallow-suppressions: {rules: [cache-operation, typo]}}`, false, false},
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
		{"timeout string", `policy: {require-job-timeout: 'true'}`, false, false},
		{"timeout zero", `policy: {require-job-timeout: {max-minutes: 0}}`, false, false},
		{"timeout negative", `policy: {require-job-timeout: {max-minutes: -1}}`, false, false},
		{"timeout null maximum", `policy: {require-job-timeout: {max-minutes: null}}`, false, false},
		{"scalar required actions", `policy: {required-actions: actions/checkout}`, false, false},
		{"empty action", `policy: {required-actions: ['']}`, false, false},
		{"non-string action", `policy: {required-actions: [1]}`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value any
			if err := yaml.Unmarshal([]byte(tt.input), &value); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(value); (err == nil) != tt.schemaValid {
				t.Errorf("schema validation error = %v, want valid = %v", err, tt.schemaValid)
			}
			if _, err := actionlint.ParseConfig([]byte(tt.input)); (err == nil) != tt.parserValid {
				t.Errorf("parser validation error = %v, want valid = %v", err, tt.parserValid)
			}
		})
	}
}

func TestSchemaDescriptions(t *testing.T) {
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
	toolSchema, err := os.ReadFile(shellcheckSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var toolDocument map[string]any
	if err := json.Unmarshal(toolSchema, &toolDocument); err != nil {
		t.Fatal(err)
	}
	check(toolDocument, "shellcheck")
}
