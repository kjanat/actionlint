package results_test

import (
	"encoding/json"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"os"
	"strings"
	"testing"
)

func TestRelatedSchemaRequiresPairedValidBinding(t *testing.T) {
	data, err := os.ReadFile("schemas/results/v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	c := validator.NewCompiler()
	const location = "https://example.test/related.schema.json"
	if err := c.AddResource(location, document); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile(location + "#/definitions/relatedLocation")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                  string
		revision, coordinates any
		valid                 bool
	}{
		{"unbound", nil, nil, true},
		{"bound", map[string]any{"document": "/repo/ci.yml", "digest": strings.Repeat("a", 64)}, "yaml", true},
		{"revision alone", map[string]any{"document": "/repo/ci.yml", "digest": strings.Repeat("a", 64)}, nil, false},
		{"coordinates alone", nil, "yaml", false},
		{"bad model", map[string]any{"document": "/repo/ci.yml", "digest": strings.Repeat("a", 64)}, "unknown", false},
		{"bad digest", map[string]any{"document": "/repo/ci.yml", "digest": strings.Repeat("A", 64)}, "raw-lf", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := map[string]any{"message": "r", "path": "ci.yml", "start": map[string]any{"line": 1, "column": 1}, "end": map[string]any{"line": 1, "column": 1}}
			if tc.revision != nil {
				value["revision"] = tc.revision
			}
			if tc.coordinates != nil {
				value["coordinates"] = tc.coordinates
			}
			if err := schema.Validate(value); (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}
}
