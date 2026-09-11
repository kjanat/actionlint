package main

import (
	"encoding/json"
	"os"
	"testing"

	"actionlint.kjanat.dev"
	"github.com/google/go-cmp/cmp"
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

func TestSchemaValidation(t *testing.T) {
	b := generatedSchema(t)
	var document any
	if err := json.Unmarshal(b, &document); err != nil {
		t.Fatal(err)
	}
	c := validator.NewCompiler()
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
						if property["markdownDescription"] != description {
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
}
