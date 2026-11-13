package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrettyCaseInsensitiveConditionConfiguration(t *testing.T) {
	t.Parallel()
	const source = `on: pull_request
permissions: {}
jobs:
  check:
    runs-on: ubuntu-latest
    if: github.head_ref != 'validity/drift'
    steps:
      - run: echo ok
`
	for _, tc := range []struct {
		name  string
		flags []string
		level string
		want  string
	}{
		{name: "default"},
		{name: "strict", flags: []string{"--strict"}, want: "error"},
		{name: "strict and experimental", flags: []string{"--strict", "--experimental"}, want: "error"},
		{name: "configured info", flags: []string{"--strict"}, level: "info", want: "info"},
		{name: "configured off", flags: []string{"--strict", "--experimental"}, level: "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := []string{
				"actionlint", "check", "--output-format=pretty", "--color=never", "--hyperlinks=never",
				"--action-metadata=off", "--no-tool=shellcheck", "--no-tool=ruff", "--quiet",
			}
			if tc.level == "" {
				args = append(args, "--no-config")
			} else {
				path := filepath.Join(t.TempDir(), "actionlint.toml")
				config := "[lint.rules.suspicious]\ncase-insensitive-conditions = \"" + tc.level + "\"\n"
				if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--config", path)
			}
			args = append(args, tc.flags...)
			args = append(args, "-")
			var out, log bytes.Buffer
			command := Command{Stdin: strings.NewReader(source), Stdout: &out, Stderr: &log}
			status := command.MainContext(t.Context(), args)
			wantStatus := 0
			if tc.want != "" {
				wantStatus = 1
			}
			if status != wantStatus {
				t.Fatalf("status=%d, want %d; output=%s log=%s", status, wantStatus, &out, &log)
			}
			if tc.want == "" {
				if strings.Contains(out.String(), "case-insensitive-conditions") {
					t.Fatalf("disabled rule emitted a finding: %s", &out)
				}
				return
			}
			for _, want := range []string{
				tc.want + "[case-insensitive-conditions]",
				"github.head_ref != 'validity/drift'",
				"  = config: lint.rules.suspicious.case-insensitive-conditions\n",
			} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q in output:\n%s", want, &out)
				}
			}
		})
	}
}
