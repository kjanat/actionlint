package actionmeta

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

func TestManifestRejectsAnchorsAndAliases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		want         []sourcetext.Position
	}{
		{"scalar", "name: &label example\ndescription: *label\nruns: {using: composite, steps: []}\n", []sourcetext.Position{{Line: 1, Column: 7}, {Line: 2, Column: 14}}},
		{"collections", "name: example\ndescription: example\nruns: &runtime\n  using: composite\n  steps: &steps []\n", []sourcetext.Position{{Line: 3, Column: 7}, {Line: 5, Column: 10}}},
		{"ignored value", "name: example\ndescription: example\nruns: {using: composite, steps: []}\nextra: {first: &value example, second: *value}\n", []sourcetext.Position{{Line: 4, Column: 16}, {Line: 4, Column: 40}}},
		{"literal text", "name: example\ndescription: 'literal &label *label'\nruns: {using: composite, steps: []}\n# &comment *comment\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta, err := ParseActionMetadata("nested/action.yaml", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			root := meta.Root
			content := slices.Clone(root.Content)
			var got []sourcetext.Position
			for _, d := range Validate(meta).Problems {
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
