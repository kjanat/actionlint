// Command layers checks the module's import graph against the layer table in
// layers.toml and writes the matching depguard rules into .golangci.toml.
//
// Run go run ./tools/layers from the module root to regenerate the rules.
// With -check, it changes nothing and fails when the graph breaks the table
// or the committed rules differ from the generated ones.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	check := flag.Bool("check", false, "fail when the import graph breaks the table or .golangci.toml is stale, without writing")
	flag.Parse()
	if err := run(context.Background(), *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, check bool) error {
	g, err := loadGraph(ctx)
	if err != nil {
		return err
	}
	specPath := filepath.Join(g.root, "tools", "layers", "layers.toml")
	configPath := filepath.Join(g.root, ".golangci.toml")
	spec, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	t, err := parseSpec(spec)
	if err != nil {
		return err
	}
	if err := t.place(g); err != nil {
		return err
	}
	if violations := checkGraph(t, g); len(violations) > 0 {
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation)
		}
		return fmt.Errorf("%d imports break the layer table in %s", len(violations), specPath)
	}
	rules := buildRules(t, g)
	if err := verifyRules(t, g, rules); err != nil {
		return fmt.Errorf("generated rules disagree with the table:\n%w", err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	updated, err := splice(config, render(rules))
	if err != nil {
		return fmt.Errorf("%s: %w", configPath, err)
	}
	switch {
	case bytes.Equal(config, updated):
		return nil
	case check:
		return fmt.Errorf("%s is stale; run go run ./tools/layers", configPath)
	default:
		return os.WriteFile(configPath, updated, 0o644)
	}
}
