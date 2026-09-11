package actionlint

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseCacheMode(t *testing.T) {
	tests := []struct {
		value string
		kind  CacheModeKind
	}{
		{"read", CacheModeRead},
		{"write", CacheModeWrite},
		{"write-only", CacheModeWriteOnly},
		{"none", CacheModeNone},
		{`"read"`, CacheModeRead},
		{`'write-only'`, CacheModeWriteOnly},
		{"READ", CacheModeInvalid},
		{"read-only", CacheModeInvalid},
		{"false", CacheModeInvalid},
		{"42", CacheModeInvalid},
		{"null", CacheModeInvalid},
		{"", CacheModeInvalid},
		{`""`, CacheModeInvalid},
		{"[]", CacheModeInvalid},
		{"{}", CacheModeInvalid},
		{"${{ github.event_name }}", CacheModeInvalid},
	}
	for _, placement := range []string{"workflow", "job", "call"} {
		for _, tt := range tests {
			t.Run(placement+"/"+tt.value, func(t *testing.T) {
				src := "on: push\n"
				wantPos := Pos{Line: 4, Col: 17}
				if placement == "workflow" {
					src += "cache-mode: " + tt.value + "\n"
					wantPos = Pos{Line: 2, Col: 13}
				}
				src += "jobs:\n  test:\n"
				if placement != "workflow" {
					src += "    cache-mode: " + tt.value + "\n"
				}
				if placement == "call" {
					src += "    uses: owner/repo/.github/workflows/reusable.yml@main\n"
				} else {
					src += "    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
				}
				w, errs := Parse([]byte(src))
				wantErrors := 0
				if tt.kind == CacheModeInvalid {
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
					if tt.value != "" && (err.Line != wantPos.Line || err.Column != wantPos.Col) {
						t.Errorf("diagnostic position is %d:%d, want %v", err.Line, err.Column, wantPos)
					}
				}
			})
		}
	}
}

func TestParseCacheModeOmittedAndAliased(t *testing.T) {
	for _, declaration := range []string{"", "cache-mode: &mode read\n"} {
		src := "on: pull_request_target\n" + declaration + "jobs:\n  test:\n    runs-on: ubuntu-latest\n"
		if declaration != "" {
			src += "    cache-mode: *mode\n"
		}
		src += "    steps:\n      - run: echo ok\n"
		w, errs := Parse([]byte(src))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		if declaration == "" {
			if w.CacheMode != nil || w.Jobs["test"].CacheMode != nil {
				t.Fatal("omitted cache mode acquired an explicit default")
			}
		} else if w.CacheMode.Kind != CacheModeRead || w.Jobs["test"].CacheMode.Kind != CacheModeRead {
			t.Fatal("cache mode alias was not resolved")
		}
	}
}

func TestParseCacheModeLowTrustWrites(t *testing.T) {
	for _, event := range []string{"pull_request_target", "issue_comment", "workflow_run"} {
		for _, mode := range []string{"write", "write-only"} {
			src := fmt.Sprintf("on: %s\ncache-mode: %s\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", event, mode)
			if _, errs := Parse([]byte(src)); len(errs) != 0 {
				t.Fatalf("explicit %s mode on %s is valid: %v", mode, event, errs)
			}
		}
	}
}
