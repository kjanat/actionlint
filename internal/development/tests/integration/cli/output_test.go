package cli_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestCommandJSONErrorsAndLogs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args     []string
		status   int
		buildLog bool
	}{
		{[]string{"--json", "--unknown"}, 2, false},
		{[]string{"--unknown", "--json"}, 2, false},
		{[]string{"--unknown", "--output", "json"}, 2, false},
		{[]string{"--unknown", "-ojson"}, 2, false},
		{[]string{"--json", "--ignore-regex", "[", "-"}, 3, false},
		{[]string{"--output=jsonl", "missing.yml"}, 3, false},
		{[]string{"--json", "--debug", "--config-file=does-not-exist.yml", "-"}, 3, true},
	}
	for _, tc := range tests {
		got := testRunCommand(commandGoodWorkflow, tc.args...)
		decoder := json.NewDecoder(strings.NewReader(got.Stderr))
		if tc.buildLog {
			for _, prefix := range []string{
				"verbose: actionlint version: ",
				"verbose: installed: ",
				"verbose: Go version: ",
				"verbose: operating system: ",
				"verbose: architecture: ",
			} {
				var record map[string]string
				if err := decoder.Decode(&record); err != nil {
					t.Fatalf("%q: invalid build log: %v", tc.args, err)
				}
				value, ok := strings.CutPrefix(record["log"], prefix)
				if len(record) != 1 || !ok || value == "" || strings.ContainsAny(value, "\r\n") {
					t.Fatalf("%q: invalid build field %q: %v", tc.args, prefix, record)
				}
			}
		}
		var message struct {
			Error         string `json:"error"`
			ExitCode      int    `json:"exit_code"`
			SchemaVersion int    `json:"schema_version"`
			Status        string `json:"status"`
			Completed     bool   `json:"completed"`
		}
		if err := decoder.Decode(&message); err != nil {
			t.Fatalf("%q: %+v: %v", tc.args, got, err)
		}
		if tc.buildLog && (message.SchemaVersion != 1 || message.Status != "failure" || message.Completed) {
			t.Fatalf("%q: invalid failure record: %+v", tc.args, message)
		}
		var extra json.RawMessage
		if err := decoder.Decode(&extra); err != io.EOF {
			t.Fatalf("%q: unexpected trailing record %q: %v", tc.args, extra, err)
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
