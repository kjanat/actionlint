package syntax_test

import (
	"testing"

	"actionlint.kjanat.dev/syntax"
)

func TestPublicCacheModeSyntax(t *testing.T) {
	for _, tc := range []struct {
		kind         syntax.CacheModeKind
		name         string
		capabilities uint8
		valid        bool
	}{
		{syntax.CacheModeInvalid, "invalid", 0, false},
		{syntax.CacheModeNone, "none", 0, true},
		{syntax.CacheModeRead, "read", 1, true},
		{syntax.CacheModeWrite, "write", 3, true},
		{syntax.CacheModeWriteOnly, "write-only", 2, true},
	} {
		mode := &syntax.CacheMode{Kind: tc.kind, Pos: &syntax.Pos{Line: 1, Col: 1}}
		capabilities, valid := mode.Capabilities()
		if tc.kind.String() != tc.name || capabilities != tc.capabilities || valid != tc.valid {
			t.Fatalf("kind=%v capabilities=%d valid=%t", tc.kind, capabilities, valid)
		}
	}
	workflow, findings := syntax.Parse([]byte("on: push\ncache-mode: read\njobs:\n  build:\n    cache-mode: write-only\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n"))
	if len(findings) != 0 || workflow == nil || workflow.CacheMode == nil || workflow.CacheMode.Kind != syntax.CacheModeRead || workflow.Jobs["build"].CacheMode.Kind != syntax.CacheModeWriteOnly {
		t.Fatalf("workflow=%+v findings=%v", workflow, findings)
	}
	workflow.CacheMode = &syntax.CacheMode{Kind: syntax.CacheModeNone}
	workflow.Jobs["build"].CacheMode = &syntax.CacheMode{Kind: syntax.CacheModeWrite}
}
