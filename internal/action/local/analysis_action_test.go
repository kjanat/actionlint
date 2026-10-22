package local

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/fsread"
)

func TestActionSnapshotManifestPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, diskYAML, diskYML, explicitYAML, explicitYML, wantName, wantContent string
	}{
		{"disk-yaml-wins", "disk yaml", "", "", "snapshot yml", "action.yaml", "disk yaml"},
		{"snapshot-yaml-wins", "disk yaml", "disk yml", "snapshot yaml", "snapshot yml", "action.yaml", "snapshot yaml"},
		{"snapshot-yml-overrides", "", "disk yml", "", "snapshot yml", "action.yml", "snapshot yml"},
		{"snapshot-yaml-before-disk-yml", "", "disk yml", "snapshot yaml", "", "action.yaml", "snapshot yaml"},
		{"disk-yml-fallback", "", "disk yml", "", "", "action.yml", "disk yml"},
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
