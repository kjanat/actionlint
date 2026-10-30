package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolverLeavesInvalidReferenceDiagnostics(t *testing.T) {
	for _, uses := range []string{"owner/@v1", "https://host/owner/repo@v1"} {
		t.Run(uses, func(t *testing.T) {
			var out, stderr bytes.Buffer
			command := Command{
				Stdin:  strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: " + uses + "\n"),
				Stdout: &out, Stderr: &stderr,
			}
			args := []string{"actionlint", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff", "--resolve-with", filepath.Join(t.TempDir(), "missing-resolver"), "-"}
			if status := command.MainContext(t.Context(), args); status != 1 || strings.Contains(stderr.String(), "resolver") || !strings.Contains(out.String(), "invalid format") {
				t.Fatalf("invalid reference lost its diagnostic: exit=%d stdout=%s stderr=%s", status, &out, &stderr)
			}
		})
	}
}
