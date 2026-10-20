package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/processconfig"
)

func windowsPathSpellings(path string) map[string]string {
	forward := filepath.ToSlash(path)
	return map[string]string{
		"native":  path,
		"forward": forward,
		"mixed":   strings.Replace(forward, "/", `\`, 1),
	}
}

func TestWindowsForwardSlashToolPaths(t *testing.T) {
	root := t.TempDir()
	tool := filepath.Join(root, "tools with spaces", "checker.exe")
	if err := os.MkdirAll(filepath.Dir(tool), 0700); err != nil {
		t.Fatal(err)
	}
	// This empty stub is used only for command-path resolution.
	if err := os.WriteFile(tool, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for spelling, path := range windowsPathSpellings(tool) {
		t.Run(spelling, func(t *testing.T) {
			resolved, arguments, err := processconfig.Resolve(path)
			if err != nil || !sameCommandFile(resolved, tool) || len(arguments) != 0 {
				t.Fatalf("literal tool: %q, %v, %v", resolved, arguments, err)
			}
			// Forward slashes also survive command-line parsing with quoted spaces.
			command := `"` + filepath.ToSlash(tool) + `" --example`
			resolved, arguments, err = processconfig.Resolve(command)
			if err != nil || !sameCommandFile(resolved, tool) || !slices.Equal(arguments, []string{"--example"}) {
				t.Fatalf("tool command: %q, %v, %v", resolved, arguments, err)
			}
			t.Setenv("ACTIONLINT_SHELLCHECK_BIN", path)
			options, err := externalToolEnvironment("SHELLCHECK")
			if err != nil {
				t.Fatal(err)
			}
			resolved, _, err = processconfig.ResolveOptions("", options)
			if err != nil || !sameCommandFile(resolved, tool) {
				t.Fatalf("environment tool: %q, %v", resolved, err)
			}
			relative := windowsPathSpellings(filepath.Join("tools with spaces", "checker.exe"))[spelling]
			resolved, _, err = processconfig.ResolveOptions("", &processconfig.Options{
				Executable: &relative, WorkingDir: filepath.ToSlash(root),
			})
			if err != nil || !sameCommandFile(resolved, tool) {
				t.Fatalf("relative tool: %q, %v", resolved, err)
			}
		})
	}
}
