package manifests_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestManifestSemanticCategories(t *testing.T) {
	t.Parallel()
	for _, category := range []string{"correctness", "suspicious", "security", "conventions"} {
		t.Run(category, func(t *testing.T) {
			t.Parallel()
			document := strings.Replace(minimal, `"category": "correctness"`, `"category": "`+category+`"`, 1)
			manifest, err := manifests.Parse([]byte(document))
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Rule.Category != category || manifest.Rule.Provider != "lint" || manifest.Rule.Maturity != "stable" {
				t.Fatalf("category changed independent metadata: %+v", manifest.Rule)
			}
		})
	}
	for _, category := range []string{"style", "policy", "external", "plugins", "nursery", "lint", ""} {
		t.Run("reject-"+category, func(t *testing.T) {
			t.Parallel()
			document := strings.Replace(minimal, `"category": "correctness"`, `"category": "`+category+`"`, 1)
			if _, err := manifests.Parse([]byte(document)); err == nil || !strings.Contains(err.Error(), "category") {
				t.Fatalf("invalid category accepted: %v", err)
			}
		})
	}
}
