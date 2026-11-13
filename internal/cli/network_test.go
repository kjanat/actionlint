package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
)

func TestNetworkStatisticsDoNotPolluteDiagnosticJSON(t *testing.T) {
	t.Parallel()
	var out, log bytes.Buffer
	command := Command{Stdin: strings.NewReader("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\npermissions: {}\n"), Stdout: &out, Stderr: &log}
	status := command.Main([]string{"actionlint", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff", "--output=json", "--summary=false", "--network-stats", "-"})
	if status != 0 || !json.Valid(out.Bytes()) {
		t.Fatalf("status=%d stdout=%s stderr=%s", status, &out, &log)
	}
	var statistics struct {
		Network accounting.Statistics `json:"network"`
	}
	if err := json.Unmarshal(log.Bytes(), &statistics); err != nil {
		t.Fatal(err)
	}
	if statistics.Network.Requests == nil || len(statistics.Network.Requests) != 0 {
		t.Fatalf("unexpected queries: %+v", statistics)
	}
}

func TestNetworkStatisticsAvailableAcrossCommands(t *testing.T) {
	t.Parallel()
	for _, commandName := range []string{"plugin", "config", "doctor", "check", "fix"} {
		t.Run(commandName, func(t *testing.T) {
			t.Parallel()
			var out, log bytes.Buffer
			command := Command{Stdout: &out, Stderr: &log}
			status := command.Main([]string{"actionlint", commandName, "--help", "--network-stats"})
			var statistics struct {
				Network *accounting.Statistics `json:"network"`
			}
			lines := bytes.Split(bytes.TrimSpace(log.Bytes()), []byte("\n"))
			if err := json.Unmarshal(lines[len(lines)-1], &statistics); err != nil || status != 0 || statistics.Network == nil {
				t.Fatalf("status=%d stderr=%s decode=%v", status, &log, err)
			}
		})
	}
}
