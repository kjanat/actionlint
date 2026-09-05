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
		name  string
		input string
		valid bool
	}{
		{"empty", `{}`, true},
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
  require-job-timeout: {max-minutes: 60.5}
  required-actions: [actions/checkout, 'github/codeql-action/*@v4*']
`, true},
		{"null settings", `
self-hosted-runner: null
config-variables: null
config-secrets: null
assume-default-permissions: null
paths: null
policy: null
`, true},
		{"null nested settings", `
self-hosted-runner: {labels: null}
paths: {'**': {ignore: null}}
policy: {require-commit-hash: null, require-job-timeout: null, required-actions: null}
`, true},
		{"empty lists", `
self-hosted-runner: {labels: []}
config-variables: []
config-secrets: []
paths: {'**': {ignore: []}}
policy: {required-actions: []}
`, true},
		{"restricted permissions", `assume-default-permissions: restricted`, true},
		{"timeout enabled", `policy: {require-job-timeout: true}`, true},
		{"timeout disabled", `policy: {require-job-timeout: false}`, true},
		{"timeout without maximum", `policy: {require-job-timeout: {}}`, true},
		{"commit hash disabled", `policy: {require-commit-hash: false}`, true},
		{"unknown setting", `config-secret: []`, false},
		{"unknown runner setting", `self-hosted-runner: {label: []}`, false},
		{"unknown path setting", `paths: {'**': {ignores: []}}`, false},
		{"unknown policy", `policy: {require-hash: true}`, false},
		{"unknown timeout setting", `policy: {require-job-timeout: {minutes: 60}}`, false},
		{"unknown permissions", `assume-default-permissions: write`, false},
		{"numeric permissions", `assume-default-permissions: 1`, false},
		{"scalar secrets", `config-secrets: TOKEN`, false},
		{"scalar regex", `paths: {'**': {ignore: 'foo'}}`, false},
		{"non-string regex", `paths: {'**': {ignore: [true]}}`, false},
		{"non-boolean policy", `policy: {require-commit-hash: 1}`, false},
		{"timeout string", `policy: {require-job-timeout: 'true'}`, false},
		{"timeout zero", `policy: {require-job-timeout: {max-minutes: 0}}`, false},
		{"timeout negative", `policy: {require-job-timeout: {max-minutes: -1}}`, false},
		{"timeout null maximum", `policy: {require-job-timeout: {max-minutes: null}}`, false},
		{"scalar required actions", `policy: {required-actions: actions/checkout}`, false},
		{"empty action", `policy: {required-actions: ['']}`, false},
		{"non-string action", `policy: {required-actions: [1]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value any
			if err := yaml.Unmarshal([]byte(tt.input), &value); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(value); (err == nil) != tt.valid {
				t.Fatalf("schema validation error = %v, want valid = %v", err, tt.valid)
			}
			if tt.valid {
				if _, err := actionlint.ParseConfig([]byte(tt.input)); err != nil {
					t.Fatalf("schema accepts a config the parser rejects: %v", err)
				}
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
