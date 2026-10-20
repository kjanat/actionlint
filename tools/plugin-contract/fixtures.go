package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Fixtures retain the same interface identity and change only one nested shape.
// Dummy functions trap, so successful rejection cannot depend on describe's data.
func generateFixtures() error {
	data, err := os.ReadFile(contractPath)
	if err != nil {
		return err
	}
	wit := strings.ReplaceAll(string(data), "\r\n", "\n")
	variants := []struct{ name, from, to string }{
		{"compatible-contract", "", ""},
		{"wrong-signature", "describe: func() -> descriptor;", "describe: func(input: input) -> descriptor;"},
		{"wrong-record", "document: string,", "document: u32,"},
		{"wrong-enum", "        source,\n        scripts,", "        scripts,\n        source,"},
	}
	dir, err := os.MkdirTemp("", "actionlint-contract-fixtures-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, variant := range variants {
		definition := wit
		if variant.from != "" {
			if !strings.Contains(definition, variant.from) {
				return fmt.Errorf("fixture %s mutation no longer matches WIT", variant.name)
			}
			definition = strings.Replace(definition, variant.from, variant.to, 1)
		}
		input := filepath.Join(dir, "analyzer.wit")
		core := filepath.Join(dir, "dummy.wasm")
		if err := os.WriteFile(input, []byte(definition), 0o600); err != nil {
			return err
		}
		output := filepath.Join("../../internal/plugins/component/testdata/contracts", variant.name+".wasm")
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		for _, args := range [][]string{
			{"component", "embed", input, "--world", "plugin", "--dummy", "-o", core},
			{"component", "new", core, "-o", output},
		} {
			command := exec.Command("wasm-tools", args...)
			command.Stdout, command.Stderr = os.Stdout, os.Stderr
			if err := command.Run(); err != nil {
				return fmt.Errorf("generate %s: %w", variant.name, err)
			}
		}
	}
	return nil
}
