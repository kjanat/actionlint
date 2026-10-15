package ruff

import (
	"bytes"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestNativeConfiguration(t *testing.T) {
	binary, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	var config Config
	if err := yaml.Unmarshal([]byte(`config:
  target-version: py312
  lint:
    select: [F]
    extend-select: [B]
    ignore: [F401]
    flake8-bugbear:
      extend-immutable-calls: [custom.Immutable]
`), &config); err != nil {
		t.Fatal(err)
	}
	native, err := config.nativeArguments()
	if err != nil {
		t.Fatal(err)
	}
	defaults := arguments(config)
	if slices.Contains(defaults, "--target-version") {
		t.Fatal("inferred target overrides native configuration")
	}
	args := append([]string{"check"}, native...)
	args = append(args, defaults[1:]...)
	cmd := exec.CommandContext(t.Context(), binary, args...)
	cmd.Stdin = strings.NewReader("import os\nprint(missing)\ndef f(items=[]):\n    return items\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	findings, err := Decode(output)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	if !slices.Equal(codes, []string{"F821", "B006"}) {
		t.Fatalf("got %v: %s", codes, output)
	}
}

func TestNativeConfigReadOnlyBoundary(t *testing.T) {
	for _, key := range UnsupportedConfigOptions() {
		config := Config{Native: map[string]any{key: nil}}
		if _, err := config.nativeArguments(); err == nil {
			t.Fatalf("accepted %s", key)
		}
	}
}
