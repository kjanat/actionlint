package local

import (
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/fsread"
)

func TestInvalidLocalActionSuppressionSourceCache(t *testing.T) {
	root := t.TempDir()
	source := "false # actionlint:ignore action\n"
	path := writeFixture(t, root, "inner/action.yml", source)
	for _, spec := range []string{"./inner", "$/inner", "./INNER", "$/INNER"} {
		t.Run(spec, func(t *testing.T) {
			base := NewCache(root, nil)
			var inputs []string
			base.reader = fsread.New(t.Context(), nil, func(path string) { inputs = append(inputs, path) })
			for attempt := range 2 {
				view := &Resolver{base: base, usedSources: make(map[string][]byte), caseInsensitive: strings.Contains(spec, "INNER")}
				metadata, cached, err := view.FindMetadata(spec)
				if metadata != nil || cached != (attempt > 0) || (err != nil) != (attempt == 0) {
					t.Fatalf("attempt %d changed failed metadata lookup: metadata=%v cached=%t err=%v", attempt, metadata, cached, err)
				}
				if len(view.usedSources) != 1 || string(view.usedSources[path]) != source {
					t.Fatalf("attempt %d lost raw metadata source: %v", attempt, view.usedSources)
				}
			}
			want := []string{path}
			if strings.Contains(spec, "INNER") {
				want = append(want, root)
			}
			slices.Sort(want)
			if got := slices.Compact(slices.Sorted(slices.Values(inputs))); !slices.Equal(got, want) {
				t.Fatalf("metadata inputs = %q, want %q", got, want)
			}
		})
	}
}
