package diagnostic

import (
	"actionlint.kjanat.dev/v2/source"
	"encoding/json"
	"strings"
	"testing"
)

func TestRelatedJSONRevisionBindingRoundTrip(t *testing.T) {
	revision, err := source.NewRevision("/repo/ci.yml", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, bound := range []bool{false, true} {
		location := Related{Message: "declaration", Path: "ci.yml", Start: Position{Line: 1, Column: 1}, End: Position{Line: 1, Column: 1}}
		if bound {
			location.Revision, location.Coordinates = revision, source.YAML
		}
		data, err := json.Marshal(location)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"revision"`) != bound || strings.Contains(string(data), `"coordinates"`) != bound {
			t.Fatalf("binding omission: %s", data)
		}
		var decoded Related
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded != location {
			t.Fatalf("round trip: %+v", decoded)
		}
	}
}
