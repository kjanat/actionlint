package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/edits"
)

func TestFixVendorLockIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ scalar, fixed string }{
		{"https://github.com/mlugg/setup-zig@v2 # comment", "mlugg/setup-zig@v2 # comment"},
		{"'https://github.com/mlugg/setup-zig@v2'", "'mlugg/setup-zig@v2'"},
		{`"https:\u002f\u002fgithub.com/mlugg/setup-zig@v2"`, `"mlugg/setup-zig@v2"`},
		{`"https://\u0067ithub.com/mlugg/setup-zig@v2"`, `"mlugg/setup-zig@v2"`},
		{"&action 'https://github.com/mlugg/setup-zig@feature/path'", "&action 'mlugg/setup-zig@feature/path'"},
		{"|-\n          https://github.com/mlugg/setup-zig/sub@v2", "|-\n          mlugg/setup-zig/sub@v2"},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.scalar+ending, func(t *testing.T) {
				t.Parallel()
				path := filepath.Join(t.TempDir(), "workflow.yml")
				before := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: " + tc.scalar + "\n"
				if strings.HasPrefix(tc.scalar, "&action ") {
					before += "      - uses: *action\n"
				}
				before += "permissions: {}\n"
				before = strings.ReplaceAll(before, "\n", ending)
				after := strings.Replace(before, strings.ReplaceAll(tc.scalar, "\n", ending), strings.ReplaceAll(tc.fixed, "\n", ending), 1)
				if err := os.WriteFile(path, []byte(before), 0600); err != nil {
					t.Fatal(err)
				}
				args := []string{"fix", "--norc", path}
				got := testRunCommand("", args...)
				var plan edits.Plan
				if err := json.Unmarshal([]byte(got.Stdout), &plan); err != nil || got.Status != 0 || len(plan.Changes) != 1 || plan.Changes[0].After != after {
					t.Fatalf("incorrect preview: %+v; plan=%+v; error=%v; want=%q", got, plan, err, after)
				}
				got = testRunCommand("", append(args, "--apply", "--quiet")...)
				written, err := os.ReadFile(path)
				if got.Status != 0 || err != nil || string(written) != after {
					t.Fatalf("incorrect applied fix: %+v; written=%q; error=%v", got, written, err)
				}
				got = testRunCommand("", args...)
				if err := json.Unmarshal([]byte(got.Stdout), &plan); err != nil || got.Status != 0 || len(plan.Changes) != 0 {
					t.Fatalf("second application changed the source: %+v; plan=%+v; error=%v", got, plan, err)
				}
			})
		}
	}
}
