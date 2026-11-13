package plugincmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"
)

func TestPluginValidateBoundsLocalManifestRead(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", authoringManifestLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	streams := commandstreams.Streams{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &bytes.Buffer{}}
	if status, err := Validate(t.Context(), streams, ValidateOptions{Path: path, JSON: true}); status != 1 || err != nil {
		t.Fatalf("status=%d error=%v stdout=%s", status, err, stdout.String())
	}
	var report authoringReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Valid || !strings.Contains(report.Error, "exceeds") {
		t.Fatalf("report=%s", stdout.String())
	}
}
