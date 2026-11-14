package process

import (
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

func TestOfflineRunsLocalProcess(t *testing.T) {
	t.Parallel()
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	execution := Execution{Cmd: executable, Args: []string{"-test.run=^TestProcessStdinHelper$"}, Env: []string{"ACTIONLINT_TEST_STDIN_HELPER=1"}, Stdin: "offline tool output"}
	output, status, err := execution.RunStatus(ctx)
	if err != nil || status != 0 || string(output) != execution.Stdin {
		t.Fatalf("output=%q status=%d error=%v", output, status, err)
	}
}
