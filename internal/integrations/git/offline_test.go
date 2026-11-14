package git

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

func TestOfflineGitDisablesLazyFetch(t *testing.T) {
	t.Parallel()
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
	input := []string{"GIT_NO_LAZY_FETCH=0", "git_terminal_prompt=1", "OTHER=value"}
	got := offlineEnvironment(ctx, input)
	want := []string{"OTHER=value", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0"}
	if !slices.Equal(got, want) {
		t.Fatalf("Git environment=%v, want %v", got, want)
	}
	if input[0] != "GIT_NO_LAZY_FETCH=0" {
		t.Fatal("caller environment changed")
	}
}
