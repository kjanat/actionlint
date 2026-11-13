package workflow_test

import (
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	workflowmodel "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestCacheModeSyntax(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind         workflowmodel.CacheModeKind
		name         string
		capabilities uint8
		valid        bool
	}{
		{workflowmodel.CacheModeInvalid, "invalid", 0, false},
		{workflowmodel.CacheModeNone, "none", 0, true},
		{workflowmodel.CacheModeRead, "read", 1, true},
		{workflowmodel.CacheModeWrite, "write", 3, true},
		{workflowmodel.CacheModeWriteOnly, "write-only", 2, true},
	} {
		mode := &workflowmodel.CacheMode{Kind: tc.kind, Pos: &workflowmodel.Pos{Line: 1, Col: 1}}
		capabilities, valid := mode.Capabilities()
		if tc.kind.String() != tc.name || capabilities != tc.capabilities || valid != tc.valid {
			t.Fatalf("kind=%v capabilities=%d valid=%t", tc.kind, capabilities, valid)
		}
	}
	workflow, findings := workflowparser.Parse([]byte("on: push\ncache-mode: read\njobs:\n  build:\n    cache-mode: write-only\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n"))
	if len(findings) != 0 || workflow == nil || workflow.CacheMode == nil || workflow.CacheMode.Kind != workflowmodel.CacheModeRead || workflow.Jobs["build"].CacheMode.Kind != workflowmodel.CacheModeWriteOnly {
		t.Fatalf("workflow=%+v findings=%v", workflow, findings)
	}
	workflow.CacheMode = &workflowmodel.CacheMode{Kind: workflowmodel.CacheModeNone}
	workflow.Jobs["build"].CacheMode = &workflowmodel.CacheMode{Kind: workflowmodel.CacheModeWrite}
}
