// Package fix validates complete edit transactions before preview or application.
package fix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/fsread"
	sourceposition "actionlint.kjanat.dev/v2/internal/source/position"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/source"
)

type Change struct {
	Path     string `json:"path"`
	Before   string `json:"before"`
	After    string `json:"after"`
	Revision string `json:"revision"`
}
type Plan struct {
	Changes []Change `json:"changes"`
	Skipped int      `json:"skipped"`
}
type edit struct {
	start, end int
	text       string
}

// Build chooses the first permitted alternative per finding. Unknown applicability
// requires explicit inclusion. Missing or inconsistent revisions are errors.
func Build(ctx context.Context, findings []diagnostic.Diagnostic, reader *fsread.Reader, includeSuggested bool) (Plan, error) {
	p := Plan{}
	edits := map[string][]edit{}
	sources := map[string][]byte{}
	for _, finding := range findings {
		for _, candidate := range finding.Fixes {
			if candidate.Applicability != "safe" && !includeSuggested {
				p.Skipped++
				continue
			}
			for _, e := range candidate.Edits {
				if err := ctx.Err(); err != nil {
					return Plan{}, err
				}
				if e.Path == "" || e.Path == "<stdin>" {
					return Plan{}, errors.New("fix has no writable source path")
				}
				path, err := filepath.Abs(e.Path)
				if err != nil {
					return Plan{}, err
				}
				content, ok := sources[path]
				if !ok {
					content, err = reader.ReadFile(path)
					if err != nil {
						return Plan{}, err
					}
					sources[path] = content
				}
				revision := candidate.Revisions[e.Path]
				if revision == "" || revision != sourceid.Digest(content) {
					return Plan{}, fmt.Errorf("fix for %s has a missing or stale source revision", e.Path)
				}
				start, end, err := sourceposition.Range(content, source.YAML, e.Start, e.End)
				if err != nil {
					return Plan{}, fmt.Errorf("%s: %w", path, err)
				}
				edits[path] = append(edits[path], edit{start, end, e.Replacement})
			}
			break
		}
	}
	for _, path := range slices.Sorted(maps.Keys(edits)) {
		items := edits[path]
		slices.SortFunc(items, func(a, b edit) int {
			if a.start != b.start {
				return a.start - b.start
			}
			return a.end - b.end
		})
		items = slices.Compact(items)
		for i := 1; i < len(items); i++ {
			if items[i].start < items[i-1].end || items[i].start == items[i-1].start {
				return Plan{}, fmt.Errorf("overlapping fixes in %s", path)
			}
		}
		before := sources[path]
		var after bytes.Buffer
		cursor := 0
		for _, e := range items {
			after.Write(before[cursor:e.start])
			after.WriteString(e.text)
			cursor = e.end
		}
		after.Write(before[cursor:])
		if after.String() != string(before) {
			p.Changes = append(p.Changes, Change{path, string(before), after.String(), sourceid.Digest(before)})
		}
	}
	return p, nil
}

// Offset converts an exclusive Unicode-scalar position to a byte offset without
// accepting a coordinate inside CRLF or outside the document.
func Offset(content []byte, p diagnostic.Position) (int, error) {
	return sourceposition.Offset(content, source.YAML, p)
}
