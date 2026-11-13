package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReadsJSONAndTOMLConfigs(t *testing.T) {
	t.Parallel()
	workflow := "on: push\njobs:\n  build:\n    runs-on: gpu\n    steps:\n      - run: echo hi\npermissions: {}\n"
	without := testRunCommand(workflow, "check", "--no-config", "-")
	if without.Status != 0 || without.Stdout != "" || without.Stderr != "" {
		t.Fatalf("without runner-label facts: %+v", without)
	}
	dir := t.TempDir()
	configs := map[string]string{
		"actionlint.toml":  "#:schema https://actionlint.kjanat.dev/schema/config.json\n# repository runner settings\nplugins = [] # explicit opt-out\n[repository] # runner section\nrunners = [\"gpu\"] # custom label\n",
		"actionlint.json":  "{\"$schema\": \"../actionlint.schema.json\", \"repository\": {\"runners\": [\"gpu\"]}}\n",
		"actionlint.jsonc": "{\n// custom runner\n\"repository\": {\"runners\": [\"gpu\"],},\n}\n",
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
		configAfter, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if name == "actionlint.toml" {
			if !strings.HasPrefix(string(configAfter), "#:schema https://actionlint.kjanat.dev/schema/config.json\n") {
				t.Fatalf("TOML read changed schema directive:\n%s", configAfter)
			}
			for _, comment := range []string{"# repository runner settings", "# explicit opt-out", "# runner section", "# custom label"} {
				if strings.Count(string(configAfter), comment) != 1 {
					t.Errorf("TOML read changed comment %q:\n%s", comment, configAfter)
				}
			}
		}
		got = testRunCommand(workflow, "check", "--config", path, "-")
		if got.Status != 0 || got.Stdout != "" || got.Stderr != "" {
			t.Fatalf("%s after migration: %+v", name, got)
		}
	}
	broken := filepath.Join(dir, "broken.toml")
	if err := os.WriteFile(broken, []byte("[repository]\nrunners = 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand(workflow, "check", "--config", broken, "-")
	if got.Status != 3 || !strings.Contains(got.Stderr, "/repository/runners:") {
		t.Fatalf("broken: %+v", got)
	}
}
