package analyzerhost

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPluginOptionsSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":1}},"required":["count"],"additionalProperties":false}`)
	for _, test := range []struct {
		options string
		valid   bool
	}{{`{"count":2}`, true}, {`{"count":0}`, false}, {`{"count":"two"}`, false}, {`{"count":2,"typo":true}`, false}, {``, false}} {
		if err := validateOptions(schema, json.RawMessage(test.options)); (err == nil) != test.valid {
			t.Fatalf("options %s: %v", test.options, err)
		}
	}
	for _, ref := range []string{"file:///tmp/private.json", "https://example.com/schema.json"} {
		err := validateOptions(json.RawMessage(`{"$ref":"`+ref+`"}`), nil)
		if err == nil || !strings.Contains(err.Error(), "must be bundled") {
			t.Fatalf("schema %s: %v", ref, err)
		}
	}
}
