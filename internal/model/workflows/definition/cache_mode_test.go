package definition_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestCacheModeSyntax(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind         definition.CacheModeKind
		name         string
		capabilities uint8
		valid        bool
	}{
		{definition.CacheModeInvalid, "invalid", 0, false},
		{definition.CacheModeNone, "none", 0, true},
		{definition.CacheModeRead, "read", 1, true},
		{definition.CacheModeWrite, "write", 3, true},
		{definition.CacheModeWriteOnly, "write-only", 2, true},
	} {
		mode := &definition.CacheMode{Kind: tc.kind, Pos: &source.Position{Line: 1, Column: 1}}
		capabilities, valid := mode.Capabilities()
		if tc.kind.String() != tc.name || capabilities != tc.capabilities || valid != tc.valid {
			t.Fatalf("kind=%v capabilities=%d valid=%t", tc.kind, capabilities, valid)
		}
	}
	workflow, findings := workflowparser.ParseWorkflow([]byte("on: push\ncache-mode: read\njobs:\n  build:\n    cache-mode: write-only\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n"))
	if len(findings) != 0 || workflow == nil || workflow.CacheMode == nil || workflow.CacheMode.Kind != definition.CacheModeRead || workflow.Jobs["build"].CacheMode.Kind != definition.CacheModeWriteOnly {
		t.Fatalf("workflow=%+v findings=%v", workflow, findings)
	}
	workflow.CacheMode = &definition.CacheMode{Kind: definition.CacheModeNone}
	workflow.Jobs["build"].CacheMode = &definition.CacheMode{Kind: definition.CacheModeWrite}
}
