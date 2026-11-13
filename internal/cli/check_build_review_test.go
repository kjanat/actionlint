package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckVerboseReportsBuildIdentity(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workflow.yml")
	if err := os.WriteFile(path, []byte("name: Test\non: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	build := commandBuild()
	identity := []string{
		"verbose: actionlint version: " + build.Version,
		"verbose: installed: " + build.InstalledFrom,
		"verbose: Go version: " + build.GoVersion,
		"verbose: operating system: " + build.OS,
		"verbose: architecture: " + build.GOARCH,
	}
	for _, tc := range []struct {
		name  string
		flags []string
		want  bool
		json  bool
	}{
		{"verbose", []string{"--verbose"}, true, false},
		{"debug", []string{"--debug"}, true, false},
		{"json", []string{"--verbose", "--json"}, true, true},
		{"quiet", []string{"--verbose", "--quiet"}, false, false},
		{"quiet json", []string{"--debug", "--quiet", "--json"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out, stderr bytes.Buffer
			command := Command{Stdout: &out, Stderr: &stderr}
			args := append([]string{"actionlint", "check", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff"}, tc.flags...)
			args = append(args, path)
			if status := command.MainContext(t.Context(), args); status != 0 {
				t.Fatalf("status=%d stdout=%s stderr=%s", status, &out, &stderr)
			}
			lines := strings.Split(stderr.String(), "\n")
			if tc.json {
				lines = nil
				decoder := json.NewDecoder(&stderr)
				for {
					var record map[string]json.RawMessage
					err := decoder.Decode(&record)
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatalf("invalid JSON log: %v", err)
					}
					if value, ok := record["log"]; ok {
						var line string
						if err := json.Unmarshal(value, &line); err != nil {
							t.Fatal(err)
						}
						lines = append(lines, line)
					}
				}
				if !json.Valid(out.Bytes()) {
					t.Fatalf("invalid JSON output: %s", &out)
				}
			}
			for _, field := range identity {
				if strings.Contains(out.String(), field) {
					t.Fatalf("build field leaked to stdout: %s", &out)
				}
				count := 0
				for _, line := range lines {
					if line == field {
						count++
					}
				}
				if (tc.want && count != 1) || (!tc.want && count != 0) {
					t.Fatalf("build field %q appeared on %d lines, stderr=%s", field, count, &stderr)
				}
			}
		})
	}
}
