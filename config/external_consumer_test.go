package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExternalConfigurationConsumer(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	consumer, err := os.ReadFile(filepath.Join("testdata", "consumer", "config_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config_test.go"), consumer, 0o600); err != nil {
		t.Fatal(err)
	}
	// File-list mode keeps this package outside the SDK's internal-import boundary
	// while reusing its module/vendor dependencies, including offline Nix builds.
	command := exec.CommandContext(t.Context(), "go", "test", "-count=1", filepath.Join(directory, "config_test.go"))
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("outside-module consumer: %v\n%s", err, output)
	}
}
