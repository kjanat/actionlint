package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"github.com/google/go-cmp/cmp"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestNetworkPolicySchema(t *testing.T) {
	t.Parallel()
	if errGeneratedPolicy != nil {
		t.Fatal(errGeneratedPolicy)
	}
	var root any
	if err := json.Unmarshal(generatedPolicy, &root); err != nil {
		t.Fatal(err)
	}
	const location = "file:///network.schema.json"
	compiler := validator.NewCompiler()
	compiler.UseLoader(validator.SchemeURLLoader{})
	if err := compiler.AddResource(location, root); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		text  string
		valid bool
	}{
		{`{}`, true},
		{`{"$schema":"https://example.com/network.schema.json"}`, true},
		{`{"defaults":{"network":{"metadata-mirrors":{"allow-unknown-repositories":true}}}}`, true},
		{`{"defaults":{"network":{"metadata-mirrors":{"allow-unknown-repositories":null}}}}`, true},
		{`{"restrictions":{"network":{"metadata-mirrors":{"exclude-services":["jsdelivr"],"exclude-repositories":["github.com/company/*"],"require-public":true}}}}`, true},
		{`{"restrictions":null}`, true},
		{`{"restrictions":{"network":{"metadata-mirrors":{"exclude-services":[],"require-public":false}}}}`, true},
		{`{"restrictions":{"network":{"metadata-mirrors":{"require-public":null}}}}`, true},
		{`{"typo":true}`, false},
		{`{"$schema":12}`, false},
		{`{"restrictions":{"network":{"metadata-mirrors":{"exclude-services":["typo"]}}}}`, false},
		{`{"restrictions":{"network":{"metadata-mirrors":{"require-public":"true"}}}}`, false},
		{`{"defaults":{"network":{"metadata-mirrors":{"services":"jsdelivr"}}}}`, false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			var value any
			if err := json.Unmarshal([]byte(tc.text), &value); err != nil {
				t.Fatal(err)
			}
			_, parseErr := config.ReadNetworkPolicy([]string{"policy.json"}, func(string) ([]byte, error) { return []byte(tc.text), nil })
			schemaErr := schema.Validate(value)
			if (parseErr == nil) != tc.valid || (schemaErr == nil) != tc.valid {
				t.Fatalf("want valid=%t; parser=%v; schema=%v", tc.valid, parseErr, schemaErr)
			}
		})
	}
	for _, name := range []string{"schemas/policy/network.schema.json", "internal/input/configuration/schema/assets/schemas/policy/network.schema.json"} {
		content, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		var stored any
		if err := json.Unmarshal(content, &stored); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(root, stored); diff != "" {
			t.Errorf("%s is stale (-generated +stored):\n%s", name, diff)
		}
	}
}
