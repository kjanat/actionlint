package actioncatalog

import (
	_ "embed"
	"encoding/json"
	"os"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed popular_actions.json
var catalogueJSON []byte

func TestBundledCatalogueSchema(t *testing.T) {
	data, err := os.ReadFile("popular_actions.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schemaDocument, document any
	if err := json.Unmarshal(data, &schemaDocument); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(catalogueJSON, &document); err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	const url = "https://example.test/popular_actions.schema.json"
	if err := c.AddResource(url, schemaDocument); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(document); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkDecode(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Decode(catalogueJSON); err != nil {
			b.Fatal(err)
		}
	}
}
