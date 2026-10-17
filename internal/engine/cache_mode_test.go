package engine

import (
	"fmt"
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestParseCacheMode(t *testing.T) {
	tests := []struct {
		value string
		kind  workflowlang.CacheModeKind
	}{
		{"read", workflowlang.CacheModeRead},
		{"write", workflowlang.CacheModeWrite},
		{"write-only", workflowlang.CacheModeWriteOnly},
		{"none", workflowlang.CacheModeNone},
		{`"read"`, workflowlang.CacheModeRead},
		{`'write-only'`, workflowlang.CacheModeWriteOnly},
		{"${{ 'read' }}", workflowlang.CacheModeRead},
		{"${{ 'write' }}", workflowlang.CacheModeWrite},
		{"${{ 'write-only' }}", workflowlang.CacheModeWriteOnly},
		{"${{ 'none' }}", workflowlang.CacheModeNone},
		{"${{ 'READ' }}", workflowlang.CacheModeInvalid},
		{"${{ '' }}", workflowlang.CacheModeInvalid},
		{"${{ true }}", workflowlang.CacheModeInvalid},
		{"${{ 1 }}", workflowlang.CacheModeInvalid},
		{"${{ null }}", workflowlang.CacheModeInvalid},
		{"${{ format('read') }}", workflowlang.CacheModeInvalid},
		{`${{ fromJSON('"read"') }}`, workflowlang.CacheModeInvalid},
		{"read-${{ 'read' }}", workflowlang.CacheModeInvalid},
		{"${{ 'read' }}-suffix", workflowlang.CacheModeInvalid},
		{"${{ 'read' }}${{ '' }}", workflowlang.CacheModeInvalid},
		{`" ${{ 'read' }} "`, workflowlang.CacheModeInvalid},
		{"${{ '${{ ''read'' }}' }}", workflowlang.CacheModeInvalid},
		{"READ", workflowlang.CacheModeInvalid},
		{"read-only", workflowlang.CacheModeInvalid},
		{"false", workflowlang.CacheModeInvalid},
		{"42", workflowlang.CacheModeInvalid},
		{"null", workflowlang.CacheModeInvalid},
		{"", workflowlang.CacheModeInvalid},
		{`""`, workflowlang.CacheModeInvalid},
		{"[]", workflowlang.CacheModeInvalid},
		{"{}", workflowlang.CacheModeInvalid},
		{"${{ github.event_name }}", workflowlang.CacheModeInvalid},
	}
	for _, placement := range []string{"workflow", "job", "call"} {
		for _, tt := range tests {
			t.Run(placement+"/"+tt.value, func(t *testing.T) {
				src := "on: push\n"
				wantPos := workflowlang.Pos{Line: 4, Col: 17}
				if placement == "workflow" {
					src += "cache-mode: " + tt.value + "\n"
					wantPos = workflowlang.Pos{Line: 2, Col: 13}
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
				w, errs := Parse([]byte(src))
				wantErrors := 0
				if tt.kind == workflowlang.CacheModeInvalid {
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
					if err.Rule != "syntax-check" || !strings.Contains(err.Message, "cache-mode") {
						t.Errorf("unexpected diagnostic: %v", err)
					}
					if tt.value != "" && (err.Start.Line != wantPos.Line || err.Start.Column != wantPos.Col) {
						t.Errorf("diagnostic position is %d:%d, want %v", err.Start.Line, err.Start.Column, wantPos)
					}
				}
			})
		}
	}
}

func TestParseCacheModeOmittedAndAliased(t *testing.T) {
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
		w, errs := Parse([]byte(src))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		if declaration == "" {
			if w.CacheMode != nil || w.Jobs["test"].CacheMode != nil {
				t.Fatal("omitted cache mode acquired an explicit default")
			}
		} else if w.CacheMode.Kind != workflowlang.CacheModeRead || w.Jobs["test"].CacheMode.Kind != workflowlang.CacheModeRead {
			t.Fatal("cache mode alias was not resolved")
		}
	}
}

func TestParseCacheModeLowTrustWrites(t *testing.T) {
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
			if _, errs := Parse([]byte(src)); len(errs) != 0 {
				t.Fatalf("explicit %s mode on %s is valid: %v", mode, event, errs)
			}
		}
	}
}
