package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckVerboseReportsBuildIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.yml")
	if err := os.WriteFile(path, []byte("name: Test\non: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	build := commandBuild()
	identity := fmt.Sprintf("verbose: actionlint %s; installed: %s; build: %s, %s/%s", build.Version, build.InstalledFrom, build.GoVersion, build.OS, build.GOARCH)
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
			var out, stderr bytes.Buffer
			command := Command{Stdout: &out, Stderr: &stderr}
			args := append([]string{"actionlint", "check", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff"}, tc.flags...)
			args = append(args, path)
			if status := command.MainContext(t.Context(), args); status != 0 {
				t.Fatalf("status=%d stdout=%s stderr=%s", status, &out, &stderr)
			}
			if strings.Contains(out.String(), identity) {
				t.Fatalf("identity leaked to stdout: %s", &out)
			}
			if got := strings.Count(stderr.String(), identity); (tc.want && got != 1) || (!tc.want && got != 0) {
				t.Fatalf("identity count %d, stderr=%s", got, &stderr)
			}
			if tc.json {
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
				}
				if !json.Valid(out.Bytes()) {
					t.Fatalf("invalid JSON output: %s", &out)
				}
			}
		})
	}
}
