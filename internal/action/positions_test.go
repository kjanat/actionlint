package action

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestMetadataPositionCacheConstruction(t *testing.T) {
	var metadata Metadata
	if err := json.Unmarshal([]byte(`{"name":"bundled","inputs":{"token":{"name":"token"}}}`), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.positions != nil {
		t.Fatal("JSON metadata allocated a source-position cache")
	}
	value := &ExprString{Value: "example", Line: 2, Column: 3}
	for range 2 {
		if got := ExpressionPosition(&metadata, value, 2); *got != (workflowlang.Pos{Line: 2, Col: 3}) || metadata.positions != nil {
			t.Fatal("source-less position lookup allocated a cache or lost its fallback")
		}
	}
	for _, content := range []string{"null\n", "name: source\nruns: {using: composite, steps: []}\n"} {
		parsed, err := Parse("nested/action.yml", []byte(content))
		if err != nil || parsed.positions == nil {
			t.Fatalf("source-backed metadata cache: %+v, %v", parsed, err)
		}
		if parsed.Path() != filepath.Join("nested", "action.yml") || string(parsed.Source) != content {
			t.Fatal("parser lost source identity")
		}
	}
}

func TestMetadataScalarSourceOwnsResults(t *testing.T) {
	const input = "name: test\nruns:\n  using: composite\n  steps:\n    - run: echo $HI # source\n      shell: bash\n"
	for _, warm := range []bool{false, true} {
		meta, err := Parse("action.yml", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		value := meta.Runs.Steps[0].Run
		if warm {
			meta.ScalarSource(value)
		}
		result := meta.ScalarSource(value)
		pristine, err := Parse("action.yml", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		want := pristine.ScalarSource(pristine.Runs.Steps[0].Run)
		if result == nil || len(result.Lines) == 0 || len(result.Spans) == 0 || len(result.Comments) == 0 {
			t.Fatal("fixture must expose mutable source data")
		}
		result.Value = "changed"
		result.ContentStart.Line = 99
		result.Lines[0].Start = 99
		result.Spans[0].YamlColumn = 99
		result.Comments[0].Text = "changed"
		if got := meta.ScalarSource(value); !reflect.DeepEqual(got, want) {
			t.Fatalf("metadata cache shares mutable source results: got %+v, want %+v", got, want)
		}
	}
}

func TestSchemaRecursiveInsertionsDoNotExpandRejectedAliases(t *testing.T) {
	const source = "name: cycle\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v4\n      with: &values\n        '${{ insert }}': *values\n        '${{insert}}': *values\n"
	meta, _ := Parse("action.yml", []byte(source))
	findings := Validate(meta)
	if len(findings) != 3 {
		t.Fatalf("recursive insertions changed physical diagnostics: %v", findings)
	}
	for _, finding := range findings {
		if !strings.Contains(finding.Message, "YAML anchors are not supported") && !strings.Contains(finding.Message, "YAML aliases are not supported") {
			t.Fatal("recursive aliases reached semantic schema validation", finding)
		}
	}
}

func TestMetadataPositionCache(t *testing.T) {
	const source = "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ${{ github_repository }}\n      shell: bash\n"
	meta, err := Parse("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	meta.Source = []byte(source)
	value := &ExprString{Value: "echo ${{ github_repository }}", Line: 6, Column: 12}
	if meta.positions == nil {
		t.Fatal("parsed metadata has no position cache")
	}
	first := meta.positions.source(meta, value)
	if first == nil {
		t.Fatal("plain scalar mapping missing")
	}
	view := meta
	parser := meta.positions.parser
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if got := view.positions.source(view, value); got == first || !reflect.DeepEqual(got, first) {
				t.Errorf("copied metadata lost mapping values or shares mutable results")
			}
			if pos := ExpressionPosition(view, value, 10); *pos != (workflowlang.Pos{Line: 6, Col: 21}) {
				t.Errorf("position = %+v", pos)
			}
		})
	}
	workers.Wait()
	if meta.positions.parser != parser {
		t.Fatal("repeated lookups replaced the scalar mapping owner")
	}
	mismatch := *value
	mismatch.Value = "different value"
	if got := meta.positions.source(meta, &mismatch); got != nil {
		t.Fatal("mapping reused for a different decoded scalar")
	}
	oldCache := meta.positions
	parsed, err := Parse("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	meta = parsed
	if meta.positions == oldCache || meta.positions.scalars != nil {
		t.Fatal("reparsing metadata left stale source mappings")
	}
}
