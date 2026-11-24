package pluginexecution

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/wasmcomponent"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
)

func TestOfflineDeferredReselectSkipsMissingRelease(t *testing.T) {
	t.Parallel()
	fixture := newDeferredReselection(t, wasmcomponent.Build(nil, []string{component.AnalyzerInterface}, nil))
	ctx, session := network.WithSession(t.Context(), network.Options{Offline: true})
	checkSkipped(ctx, t, fixture.engine, session, "is not cached")
	if fixture.engine.next != nil {
		t.Fatalf("offline reselection chose %+v", fixture.engine.next)
	}
}
