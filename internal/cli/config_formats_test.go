package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReadsJSONAndTOMLConfigs(t *testing.T) {
	workflow := "on: push\njobs:\n  build:\n    runs-on: gpu\n    steps:\n      - run: echo hi\n"
	without := testRunCommand(workflow, "check", "--no-config", "-")
	if without.Status != 1 || !strings.Contains(without.Stdout, "[runner-label]") {
		t.Fatalf("without config: %+v", without)
	}
	dir := t.TempDir()
	configs := map[string]string{
		"actionlint.toml": "[self-hosted-runner]\nlabels = [\"gpu\"]\n",
		"actionlint.json": "{\"$schema\": \"../actionlint.schema.json\", \"self-hosted-runner\": {\"labels\": [\"gpu\"]}}\n",
	}
	for name, content := range configs {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got := testRunCommand(workflow, "check", "--config", path, "-")
		if got.Status != 0 || got.Stdout != "" || got.Stderr != "" {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	broken := filepath.Join(dir, "broken.toml")
	if err := os.WriteFile(broken, []byte("[self-hosted-runner]\nlabels = 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand(workflow, "check", "--config", broken, "-")
	if got.Status != 3 || !strings.Contains(got.Stderr, "line 2: cannot construct !!int `7` into []string") {
		t.Fatalf("broken: %+v", got)
	}
}
