package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Selection selects an explicit file, repository discovery, or no configuration.
type Selection struct {
	Path     string
	Disabled bool
}

// SelectedPath finds the selected config without parsing its contents.
func SelectedPath(selection Selection) (string, error) {
	if selection.Disabled {
		return "", nil
	}
	if selection.Path != "" {
		return selection.Path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := repositoryRoot(cwd)
	if root == "" {
		return "", nil
	}
	for _, name := range RepositoryConfigNames {
		path := filepath.Join(root, ".github", name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", nil
}

// Inspect validates settings and optionally records their YAML origins.
func (ruleSet RuleSet) Inspect(selection Selection, withOrigin bool, registeredRules ...string) (Inspection, error) {
	return ruleSet.InspectContext(context.Background(), selection, withOrigin, registeredRules...)
}

// InspectContext validates settings with cancellable configuration acquisition.
func (ruleSet RuleSet) InspectContext(ctx context.Context, selection Selection, withOrigin bool, registeredRules ...string) (Inspection, error) {
	return ruleSet.inspectContext(ctx, selection, withOrigin, nil, nil, nil, registeredRules...)
}

// InspectContextWithRegistrations validates settings using the selected rules and tool replacements.
func (ruleSet RuleSet) InspectContextWithRegistrations(ctx context.Context, selection Selection, withOrigin bool, readFile func(string) ([]byte, error), policy *NetworkPolicy, registrations ...RuleRegistration) (Inspection, error) {
	metadata := make([]RuleRegistration, len(registrations))
	copy(metadata, registrations)
	names := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		names = append(names, registration.Descriptor.Name)
	}
	return ruleSet.inspectContext(ctx, selection, withOrigin, readFile, metadata, policy, names...)
}

func (ruleSet RuleSet) inspectContext(ctx context.Context, selection Selection, withOrigin bool, readFile func(string) ([]byte, error), registrations []RuleRegistration, policy *NetworkPolicy, registeredRules ...string) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	path, err := SelectedPath(selection)
	if err != nil {
		return Inspection{}, err
	}
	parseRules := ruleSet
	if registrations != nil {
		parseRules, err = ruleSet.WithRegisteredRules(registrations...)
		if err != nil {
			return Inspection{Path: path}, fmt.Errorf("could not bind config file %q: %w", path, err)
		}
	}
	var resolved Resolved
	if path != "" {
		var source *Loaded
		source, err = parseRules.ReadContext(ctx, path, readFile, registeredRules...)
		if err != nil {
			return Inspection{Path: path}, err
		}
		resolved = source.Resolved
	} else {
		resolved, err = parseRules.ResolveDocument(nil, registeredRules...)
	}
	if err != nil {
		return Inspection{Path: path}, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	if registrations != nil {
		if _, err := ruleSet.ResolveRegisteredRules(Presets{}, resolved.Config, registrations...); err != nil {
			return Inspection{Path: path}, fmt.Errorf("could not bind config file %q: %w", path, err)
		}
	}
	resolved, err = policy.Resolve(parseRules, resolved)
	if err != nil {
		return Inspection{Path: path}, err
	}
	result := Inspection{Path: path, Config: resolved.Values, Warnings: resolved.Warnings}
	if withOrigin {
		result.Origins = resolved.Origins
	}
	return result, nil
}

func repositoryRoot(start string) string {
	dir := absPath(start)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
