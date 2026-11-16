package parser_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestParseCacheMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		kind  definition.CacheModeKind
	}{
		{"read", definition.CacheModeRead},
		{"write", definition.CacheModeWrite},
		{"write-only", definition.CacheModeWriteOnly},
		{"none", definition.CacheModeNone},
		{`"read"`, definition.CacheModeRead},
		{`'write-only'`, definition.CacheModeWriteOnly},
		{"${{ 'read' }}", definition.CacheModeRead},
		{"${{ 'write' }}", definition.CacheModeWrite},
		{"${{ 'write-only' }}", definition.CacheModeWriteOnly},
		{"${{ 'none' }}", definition.CacheModeNone},
		{"${{ 'READ' }}", definition.CacheModeInvalid},
		{"${{ '' }}", definition.CacheModeInvalid},
		{"${{ true }}", definition.CacheModeInvalid},
		{"${{ 1 }}", definition.CacheModeInvalid},
		{"${{ null }}", definition.CacheModeInvalid},
		{"${{ format('read') }}", definition.CacheModeInvalid},
		{`${{ fromJSON('"read"') }}`, definition.CacheModeInvalid},
		{"read-${{ 'read' }}", definition.CacheModeInvalid},
		{"${{ 'read' }}-suffix", definition.CacheModeInvalid},
		{"${{ 'read' }}${{ '' }}", definition.CacheModeInvalid},
		{`" ${{ 'read' }} "`, definition.CacheModeInvalid},
		{"${{ '${{ ''read'' }}' }}", definition.CacheModeInvalid},
		{"READ", definition.CacheModeInvalid},
		{"read-only", definition.CacheModeInvalid},
		{"false", definition.CacheModeInvalid},
		{"42", definition.CacheModeInvalid},
		{"null", definition.CacheModeInvalid},
		{"", definition.CacheModeInvalid},
		{`""`, definition.CacheModeInvalid},
		{"[]", definition.CacheModeInvalid},
		{"{}", definition.CacheModeInvalid},
		{"${{ github.event_name }}", definition.CacheModeInvalid},
	}
	for _, placement := range []string{"workflow", "job", "call"} {
		for _, tt := range tests {
			t.Run(placement+"/"+tt.value, func(t *testing.T) {
				t.Parallel()
				src := "on: push\n"
				wantPos := source.Position{Line: 4, Column: 17}
				if placement == "workflow" {
					src += "cache-mode: " + tt.value + "\n"
					wantPos = source.Position{Line: 2, Column: 13}
				}
				src += `jobs:
  test:
`
				if placement != "workflow" {
					src += "    cache-mode: " + tt.value + "\n"
				}
				if placement == "call" {
					src += "    uses: owner/repo/.github/workflows/reusable.yml@main\n"
				} else {
					src += `    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
				}
				w, errs := parser.ParseWorkflow([]byte(src))
				wantErrors := 0
				if tt.kind == definition.CacheModeInvalid {
					wantErrors = 1
				}
				if len(errs) != wantErrors {
					t.Fatalf("wanted %d errors, got %v", wantErrors, errs)
				}
				mode := w.CacheMode
				if placement != "workflow" {
					mode = w.Jobs["test"].CacheMode
				}
				if mode == nil || mode.Kind != tt.kind {
					t.Fatalf("got mode %v, want %v", mode, tt.kind)
				}
				if tt.value != "" && *mode.Pos != wantPos {
					t.Errorf("mode position is %v, want %v", mode.Pos, wantPos)
				}
				for _, err := range errs {
					if err.Kind != "syntax-check" || !strings.Contains(err.Message, "cache-mode") {
						t.Errorf("unexpected diagnostic: %v", err)
					}
					if tt.value != "" && (err.Line != wantPos.Line || err.Column != wantPos.Column) {
						t.Errorf("diagnostic position is %d:%d, want %v", err.Line, err.Column, wantPos)
					}
				}
			})
		}
	}
}

func TestParseCacheModeOmittedAndAliased(t *testing.T) {
	t.Parallel()
	for _, declaration := range []string{"", "cache-mode: &mode read\n"} {
		src := "on: pull_request_target\n" + declaration + `jobs:
  test:
    runs-on: ubuntu-latest
`
		if declaration != "" {
			src += "    cache-mode: *mode\n"
		}
		src += `    steps:
      - run: echo ok
`
		w, errs := parser.ParseWorkflow([]byte(src))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		if declaration == "" {
			if w.CacheMode != nil || w.Jobs["test"].CacheMode != nil {
				t.Fatal("omitted cache mode acquired an explicit default")
			}
		} else if w.CacheMode.Kind != definition.CacheModeRead || w.Jobs["test"].CacheMode.Kind != definition.CacheModeRead {
			t.Fatal("cache mode alias was not resolved")
		}
	}
}

func TestParseCacheModeLowTrustWrites(t *testing.T) {
	t.Parallel()
	for _, event := range []string{"pull_request_target", "issue_comment", "workflow_run"} {
		for _, mode := range []string{"write", "write-only"} {
			src := fmt.Sprintf(`on: %s
cache-mode: %s
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`, event, mode)
			if _, errs := parser.ParseWorkflow([]byte(src)); len(errs) != 0 {
				t.Fatalf("explicit %s mode on %s is valid: %v", mode, event, errs)
			}
		}
	}
}
