package plugincommand

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/output"
)

func TestPluginValidateBoundsLocalManifestRead(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", authoringManifestLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	streams := output.Streams{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &bytes.Buffer{}}
	if status, err := (ValidateOptions{Path: path, JSON: true}).Run(t.Context(), streams, dependencycache.Acquisition{}); status != 1 || err != nil {
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
