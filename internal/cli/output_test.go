package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestCommandJSONErrorsAndLogs(t *testing.T) {
	tests := []struct {
		args   []string
		status int
	}{
		{[]string{"--json", "--unknown"}, 2},
		{[]string{"--unknown", "--json"}, 2},
		{[]string{"--unknown", "--output", "json"}, 2},
		{[]string{"--unknown", "-ojson"}, 2},
		{[]string{"--json", "--ignore", "[", "-"}, 3},
		{[]string{"--output=jsonl", "missing.yml"}, 3},
		{[]string{"--json", "--debug", "--config-file=does-not-exist.yml", "-"}, 3},
	}
	for _, tc := range tests {
		got := testRunCommand(commandGoodWorkflow, tc.args...)
		var message struct {
			Error    string `json:"error"`
			ExitCode int    `json:"exit_code"`
		}
		if err := json.Unmarshal([]byte(got.Stderr), &message); err != nil {
			t.Fatalf("%q: %+v: %v", tc.args, got, err)
		}
		if got.Stdout != "" || got.Status != tc.status || message.ExitCode != tc.status || message.Error == "" {
			t.Fatalf("%q: %+v", tc.args, got)
		}
	}
	for _, flag := range []string{"--verbose", "--debug"} {
		got := testRunCommand(commandBadWorkflow, "--json", flag, "-")
		if !json.Valid([]byte(got.Stdout)) || got.Stderr == "" {
			t.Fatalf("%+v", got)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(got.Stderr), "\n") {
			var record map[string]string
			if err := json.Unmarshal([]byte(line), &record); err != nil || record["log"] == "" {
				t.Fatalf("not a JSON log record: %q (%v)", line, err)
			}
		}
		quiet := testRunCommand(commandBadWorkflow, "--json", flag, "-q", "-")
		if quiet.Stdout != got.Stdout || quiet.Status != 1 || quiet.Stderr != "" {
			t.Fatalf("quiet suppressed findings or leaked logs: %+v", quiet)
		}
	}
}

func TestCommandLogRecords(t *testing.T) {
	var out bytes.Buffer
	w := commandJSONLogWriter{out: &out}
	_, _ = io.WriteString(&w, "first ")
	_, _ = io.WriteString(&w, "line\nsecond line\n")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() { _, _ = fmt.Fprintf(&w, "worker %d\n", i) })
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 22 {
		t.Fatal(len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal(line)
		}
	}
}
