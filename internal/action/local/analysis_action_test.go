package local

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/fsread"
)

func TestActionSnapshotManifestPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, diskYAML, diskYML, explicitYAML, explicitYML, wantName, wantContent string
	}{
		{"snapshot-yml-before-disk-yaml", "disk yaml", "", "", "snapshot yml", "action.yml", "snapshot yml"},
		{"snapshot-yml-wins", "disk yaml", "disk yml", "snapshot yaml", "snapshot yml", "action.yml", "snapshot yml"},
		{"snapshot-yml-overrides", "", "disk yml", "", "snapshot yml", "action.yml", "snapshot yml"},
		{"disk-yml-before-snapshot-yaml", "", "disk yml", "snapshot yaml", "", "action.yml", "disk yml"},
		{"disk-yml-fallback", "", "disk yml", "", "", "action.yml", "disk yml"},
		{"disk-yaml-fallback", "disk yaml", "", "", "", "action.yaml", "disk yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cache := NewCache(root, nil)
			cache.explicitSources = make(map[string][]byte)
			for name, content := range map[string]string{"action.yaml": tc.diskYAML, "action.yml": tc.diskYML} {
				if content != "" {
					writeFixture(t, root, name, content)
				}
			}
			for name, content := range map[string]string{"action.yaml": tc.explicitYAML, "action.yml": tc.explicitYML} {
				if content != "" {
					cache.explicitSources[fsread.PhysicalPath(filepath.Join(root, name))] = []byte(content)
				}
			}
			content, name, err := cache.readLocalActionMetadataFile(root)
			if err != nil || name != tc.wantName || string(content) != tc.wantContent {
				t.Fatalf("want %s containing %q; got %s containing %q, error: %v", tc.wantName, tc.wantContent, name, content, err)
			}
		})
	}
}
