package application

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRemoteConfigWithSourceOverlay(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "actionlint.yaml")
	remote := "https://example.test/base.yaml?version=1"
	reads := 0
	result, err := Check(t.Context(), Options{
		WorkingDir:     root,
		ConfigFile:     leaf,
		SourceOverlays: []SourceOverlay{{Path: filepath.Join(root, "buffer.yml"), Content: []byte("on: push\njobs: {}\n")}},
		ReadFile: func(name string) ([]byte, error) {
			switch name {
			case leaf:
				return []byte("extends: [\"" + remote + "\"]"), nil
			case remote:
				reads++
				return []byte("policy: {require-commit-hash: true}"), nil
			default:
				return nil, fmt.Errorf("unexpected source %q", name)
			}
		},
	}, Input{Kind: Stdin, Reader: strings.NewReader("on: push\njobs: {}\n"), Name: "workflow.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 || !slices.Contains(result.Inputs, remote) {
		t.Fatalf("remote reads=%d, inputs=%v", reads, result.Inputs)
	}
}
