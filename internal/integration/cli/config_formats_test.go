package cli_test

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
		"actionlint.toml": "#:schema https://actionlint.kjanat.dev/schema/config.json\n# repository runner settings\nplugins = [] # explicit opt-out\n[self-hosted-runner] # runner section\nlabels = [\"gpu\"] # custom label\n",
		"actionlint.json": "{\"$schema\": \"../actionlint.schema.json\", \"self-hosted-runner\": {\"labels\": [\"gpu\"]}}\n",
	}
	for name, content := range configs {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		physical, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		got := testRunCommand(workflow, "check", "--config", path, "-")
		if got.Status != 0 || got.Stdout != "" || got.Stderr != "warning: migrated configuration: "+physical+"\n" {
			t.Fatalf("%s: %+v", name, got)
		}
		migrated, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if name == "actionlint.toml" {
			if !strings.HasPrefix(string(migrated), "#:schema https://actionlint.kjanat.dev/schema/config.json\n") {
				t.Fatalf("automatic TOML migration removed schema directive:\n%s", migrated)
			}
			for _, comment := range []string{"# repository runner settings", "# explicit opt-out", "# runner section", "# custom label"} {
				if strings.Count(string(migrated), comment) != 1 {
					t.Errorf("automatic TOML migration changed comment %q:\n%s", comment, migrated)
				}
			}
		}
		got = testRunCommand(workflow, "check", "--config", path, "-")
		if got.Status != 0 || got.Stdout != "" || got.Stderr != "" {
			t.Fatalf("%s after migration: %+v", name, got)
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
