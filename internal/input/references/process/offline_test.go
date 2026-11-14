package process

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

func TestOfflineDoesNotStartResolver(t *testing.T) {
	t.Parallel()
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
	resolver := NewProcess("actionlint-nonexistent-resolver", nil, 0)
	_, err := resolver.Resolve(ctx, references.Request{Version: 1, Repository: "example/action", Ref: "v1", Kind: "action"})
	if !errors.Is(err, network.ErrOffline) {
		t.Fatalf("expected offline rejection before process start, got %v", err)
	}
}
