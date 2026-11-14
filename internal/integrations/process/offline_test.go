package process

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

func TestOfflineRejectsProcessBeforeExecutableLookup(t *testing.T) {
	t.Parallel()
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
	execution := Execution{Cmd: "actionlint-nonexistent-executable"}
	_, _, err := execution.RunStatus(ctx)
	if !errors.Is(err, network.ErrOffline) {
		t.Fatalf("expected offline rejection before executable lookup, got %v", err)
	}
}
