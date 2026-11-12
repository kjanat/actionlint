package results

import (
	"encoding/json"
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

func TestRelatedJSONRevisionBindingRoundTrip(t *testing.T) {
	t.Parallel()
	revision, err := sourcepos.NewRevision("/repo/ci.yml", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, bound := range []bool{false, true} {
		location := Related{Message: "declaration", Path: "ci.yml", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 1}}
		if bound {
			location.Revision, location.Coordinates = revision, sourcepos.YAML
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
