package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/availability"

	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestActionMetadataCLIReportsDisabledCoverage(t *testing.T) {
	var out, log bytes.Buffer
	command := Command{Stdin: strings.NewReader("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n"), Stdout: &out, Stderr: &log}
	code := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-config", "--output-format=json", "--summary=false", "-"})
	if code != 0 {
		t.Fatalf("status=%d stderr=%s output=%s", code, log.String(), out.String())
	}
	var result resultreport.CheckResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Reason != availability.LookupDisabledReason {
		t.Fatalf("coverage=%v", result.ActionMetadata)
	}
}

func TestActionMetadataModeRejectsUnknownValue(t *testing.T) {
	if _, err := (checkInvocation{ActionMetadata: "sometimes"}).actionMetadata(); err == nil {
		t.Fatal("unknown network policy accepted")
	}
}
