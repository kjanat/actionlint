package source

import "testing"

func TestCoordinates(t *testing.T) {
	for _, model := range []Coordinates{YAML, RawLF} {
		if !model.Valid() {
			t.Errorf("invalid supported model %q", model)
		}
	}
	for _, model := range []Coordinates{"", "unknown"} {
		if model.Valid() {
			t.Errorf("accepted unsupported model %q", model)
		}
	}
}
