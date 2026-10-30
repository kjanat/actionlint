package architecture_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Preparation profiles are language preparation shared by every tool
// provider. Settings reach them as typed values, so they never acquire
// configuration loading or tool registration, which imports it, through an
// indirect import.
func TestPreparationProfilesDoNotImportConfiguration(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/v2/internal/tools/profile/...").Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	profiles := 0
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Deps       []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		profiles++
		for _, dependency := range pkg.Deps {
			if dependency == "actionlint.kjanat.dev/v2/internal/config" || strings.HasPrefix(dependency, "actionlint.kjanat.dev/v2/internal/config/") || dependency == "actionlint.kjanat.dev/v2/internal/tools" {
				t.Errorf("preparation profile %s imports %s", pkg.ImportPath, dependency)
			}
		}
	}
	if profiles == 0 {
		t.Fatal("no preparation profiles listed")
	}
}
