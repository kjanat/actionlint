package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteConfigPaths(t *testing.T) {
	for _, test := range []struct {
		name, document, want string
	}{
		{"relative-command", "tools: {shellcheck: {backend: {process: {executable: ./shellcheck}}}}", "relative filesystem"},
		{"relative-module", "tools: {shellcheck: {backend: {wasm: {path: ./shellcheck.wasm}}}}", "relative filesystem"},
		{"relative-tool-config", "tools: {shellcheck: {config: .shellcheckrc}}", "relative filesystem"},
		{"configdir", `tools: {shellcheck: {config: "${{ configdir }}/.shellcheckrc"}}`, "configdir"},
		{"inline-configdir", `tools: {shellcheck: {config: {source-path: ["${{ configdir }}/sources"]}}}`, "configdir"},
		{"command-name", "tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}", ""},
		{"workspace-tool-config", `tools: {shellcheck: {config: "${{ github.workspace }}/.shellcheckrc"}}`, ""},
		{"local-actions", "local-actions: {./generated: ./source}", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "actionlint.yaml")
			remote := "https://example.test/base.yaml"
			loaded, err := Read(path, func(name string) ([]byte, error) {
				switch name {
				case path:
					return []byte("extends: [" + remote + "]"), nil
				case remote:
					return []byte(test.document), nil
				default:
					return nil, fmt.Errorf("unexpected source %q", name)
				}
			})
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), remote) {
					t.Fatalf("error = %v, want %q and origin", err, test.want)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if loaded.Config.Sources()[0] != remote {
				t.Fatalf("sources = %v", loaded.Config.Sources())
			}
		})
	}
}
