package process

import (
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestOfflineRunsNativeTool(t *testing.T) {
	t.Parallel()
	command := helperCommand(t, "echo")
	backend, err := NewBackend(helperManifest(manifests.ProcessSpec{Entry: "helper", Stdin: manifests.StdinSource, Output: echoOutput}), command, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, session := network.WithSession(t.Context(), network.Options{Offline: true})
	for range 2 {
		result, err := backend.Run(ctx, checkinputs.ScriptRequest{Source: "echo ok", Filename: "test.sh"}, nil)
		if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "ECHO" {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	}
	if warnings := session.Warnings(); len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}
