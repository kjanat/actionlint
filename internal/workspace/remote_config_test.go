package workspace_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestRemoteConfigInputIdentity(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "actionlint.yaml")
	remote := "https://example.test/base.toml?version=1"
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, ConfigFile: leaf, ReadFile: func(name string) ([]byte, error) {
		switch name {
		case leaf:
			return []byte("extends: [\"" + remote + "\"]"), nil
		case remote:
			return []byte("[policy]\nrequire-commit-hash = true"), nil
		default:
			return nil, fmt.Errorf("unexpected source %q", name)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := session.Empty()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(selection.Inputs, remote) {
		t.Fatalf("remote identity absent from inputs: %v", selection.Inputs)
	}
}
