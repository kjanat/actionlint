package action

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results"
)

func TestManifestRejectsAnchorsAndAliases(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []results.Position
	}{
		{"scalar", "name: &label example\ndescription: *label\nruns: {using: composite, steps: []}\n", []results.Position{{Line: 1, Column: 7}, {Line: 2, Column: 14}}},
		{"collections", "name: example\ndescription: example\nruns: &runtime\n  using: composite\n  steps: &steps []\n", []results.Position{{Line: 3, Column: 7}, {Line: 5, Column: 10}}},
		{"ignored value", "name: example\ndescription: example\nruns: {using: composite, steps: []}\nextra: {first: &value example, second: *value}\n", []results.Position{{Line: 4, Column: 16}, {Line: 4, Column: 40}}},
		{"literal text", "name: example\ndescription: 'literal &label *label'\nruns: {using: composite, steps: []}\n# &comment *comment\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta, err := Parse("nested/action.yaml", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			root := meta.Root
			content := slices.Clone(root.Content)
			var got []results.Position
			for _, d := range Validate(meta) {
				if strings.Contains(d.Message, "YAML anchors") || strings.Contains(d.Message, "YAML aliases") {
					if d.Code != CodeYAMLAnchor && d.Code != CodeYAMLAlias || d.Path != filepath.Join("nested", "action.yaml") {
						t.Fatalf("manifest diagnostic lost source identity: %+v", d)
					}
					got = append(got, d.Start)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("anchor/alias locations = %v, want %v", got, tc.want)
			}
			if meta.Root != root || !slices.Equal(root.Content, content) {
				t.Fatal("manifest validation changed source nodes")
			}
		})
	}
}
