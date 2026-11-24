package actionmeta

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

func TestMetadataPositionCacheConstruction(t *testing.T) {
	t.Parallel()
	var metadata Metadata
	if err := json.Unmarshal([]byte(`{"name":"bundled","inputs":{"token":{"name":"token"}}}`), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.manifest != nil {
		t.Fatal("JSON metadata has manifest text")
	}
	value := &ExprString{Value: "example", Position: sourcetext.Position{Line: 2, Column: 3}}
	for range 2 {
		if got := ExpressionPosition(&metadata, value, 2); *got != (sourcetext.Position{Line: 2, Column: 3}) || metadata.manifest != nil {
			t.Fatal("source-less position lookup allocated a cache or lost its fallback")
		}
	}
	for _, content := range []string{"null\n", "name: source\nruns: {using: composite, steps: []}\n"} {
		parsed, err := ParseActionMetadata("nested/action.yml", []byte(content))
		if err != nil || parsed.manifest == nil {
			t.Fatalf("source-backed metadata cache: %+v, %v", parsed, err)
		}
		if parsed.Path() != filepath.Join("nested", "action.yml") || parsed.manifest.text != content {
			t.Fatal("parser lost source identity")
		}
	}
}

func TestScriptSourceResultEditsLeaveCacheUnchanged(t *testing.T) {
	t.Parallel()
	const input = "name: test\nruns:\n  using: composite\n  steps:\n    - run: echo $HI # source\n      shell: bash\n"
	for _, warm := range []bool{false, true} {
		meta, err := ParseActionMetadata("action.yml", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		value := meta.Runs.Steps[0].Run
		if warm {
			meta.manifest.scriptSource(value)
		}
		result := meta.manifest.scriptSource(value)
		pristine, err := ParseActionMetadata("action.yml", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		want := pristine.manifest.scriptSource(pristine.Runs.Steps[0].Run)
		if result == nil || len(result.Lines) == 0 || len(result.Spans) == 0 || len(result.Comments) == 0 {
			t.Fatal("fixture must expose mutable source data")
		}
		result.Value = "changed"
		result.ContentStart.Line = 99
		result.Lines[0].Start = 99
		result.Spans[0].Document.Column = 99
		result.Comments[0].Text = "changed"
		if got := meta.manifest.scriptSource(value); !reflect.DeepEqual(got, want) {
			t.Fatalf("metadata cache shares mutable source results: got %+v, want %+v", got, want)
		}
	}
}

func TestSchemaRecursiveInsertionsDoNotExpandRejectedAliases(t *testing.T) {
	t.Parallel()
	const source = "name: cycle\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v4\n      with: &values\n        '${{ insert }}': *values\n        '${{insert}}': *values\n"
	meta, _ := ParseActionMetadata("action.yml", []byte(source))
	findings := Validate(meta).Problems
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
	t.Parallel()
	const source = "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ${{ github_repository }}\n      shell: bash\n"
	meta, err := ParseActionMetadata("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	meta.Root = nil
	value := &ExprString{Value: "echo ${{ github_repository }}", Position: sourcetext.Position{Line: 6, Column: 12}}
	if meta.manifest == nil {
		t.Fatal("parsed metadata has no manifest text")
	}
	first := meta.manifest.scriptSource(value)
	if first == nil {
		t.Fatal("plain scalar mapping missing")
	}
	view := *meta
	parser := meta.manifest.parser
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if got := view.manifest.scriptSource(value); got == first || !reflect.DeepEqual(got, first) {
				t.Errorf("copied metadata lost mapping values or shares mutable results")
			}
			if pos := ExpressionPosition(&view, value, 10); *pos != (sourcetext.Position{Line: 6, Column: 21}) {
				t.Errorf("position = %+v", pos)
			}
		})
	}
	workers.Wait()
	if meta.manifest.parser != parser {
		t.Fatal("repeated lookups replaced the scalar mapping owner")
	}
	mismatch := *value
	mismatch.Value = "different value"
	if got := meta.manifest.scriptSource(&mismatch); got != nil {
		t.Fatal("mapping reused for a different decoded scalar")
	}
	oldIndex := meta.manifest
	parsed, err := ParseActionMetadata("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	meta = parsed
	if meta.manifest == oldIndex || meta.manifest.indexed {
		t.Fatal("reparsing metadata left stale source mappings")
	}
}
