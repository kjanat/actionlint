package actionlint

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

type configOrigin struct {
	Source string `json:"source" yaml:"source"`
	State  string `json:"state" yaml:"state"`
	Line   int    `json:"line,omitempty" yaml:"line,omitempty"`
	Column int    `json:"column,omitempty" yaml:"column,omitempty"`
}

type configInspection struct {
	Path    string                  `json:"path" yaml:"path"`
	Config  map[string]any          `json:"config" yaml:"config"`
	Origins map[string]configOrigin `json:"origins,omitempty" yaml:"origins,omitempty"`
}

func selectedConfigPath(selection ConfigSelection) (string, error) {
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
	project, err := findProjectConfig(cwd, true)
	if err != nil || project == nil {
		return "", err
	}
	for _, name := range []string{"actionlint.yaml", "actionlint.yml"} {
		path := filepath.Join(project.RootDir(), ".github", name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", nil
}

func inspectConfig(selection ConfigSelection, withOrigin bool) (configInspection, error) {
	path, err := selectedConfigPath(selection)
	if err != nil {
		return configInspection{}, err
	}
	cfg := &Config{}
	var document yaml.Node
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return configInspection{Path: path}, fmt.Errorf("could not read config file %q: %w", path, err)
		}
		cfg, err = ParseConfig(data)
		if err != nil {
			return configInspection{Path: path}, fmt.Errorf("could not parse config file %q: %w", path, err)
		}
		if withOrigin {
			if err := yaml.Unmarshal(data, &document); err != nil {
				return configInspection{}, err
			}
		}
	}
	result := configInspection{Path: path, Config: effectiveConfig(cfg)}
	if withOrigin {
		result.Origins = map[string]configOrigin{}
		var defaults func(map[string]any, string)
		defaults = func(values map[string]any, prefix string) {
			for key, value := range values {
				pointer := prefix + "/" + configPointerPart(key)
				result.Origins[pointer] = configOrigin{Source: "default", State: "missing"}
				if nested, ok := value.(map[string]any); ok {
					defaults(nested, pointer)
				}
			}
		}
		defaults(result.Config, "")
		var origins func(*yaml.Node, string)
		visiting := map[*yaml.Node]bool{}
		origins = func(node *yaml.Node, prefix string) {
			if visiting[node] {
				return
			}
			visiting[node] = true
			defer delete(visiting, node)
			if node.Kind == yaml.AliasNode && node.Alias != nil {
				origins(node.Alias, prefix)
				return
			}
			if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
				origins(node.Content[0], prefix)
				return
			}
			if node.Kind == yaml.SequenceNode {
				for _, item := range slices.Backward(node.Content) {
					origins(item, prefix)
				}
				return
			}
			if node.Kind != yaml.MappingNode {
				return
			}
			for i := 0; i+1 < len(node.Content); i += 2 {
				if node.Content[i].Tag == "!!merge" {
					origins(node.Content[i+1], prefix)
				}
			}
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				pointer := prefix + "/" + configPointerPart(key.Value)
				if _, used := result.Origins[pointer]; !used {
					continue
				}
				state := "value"
				resolved := value
				if resolved.Kind == yaml.AliasNode && resolved.Alias != nil {
					resolved = resolved.Alias
				}
				if resolved.Tag == "!!null" {
					state = "null"
				}
				result.Origins[pointer] = configOrigin{Source: "config", State: state, Line: value.Line, Column: value.Column}
				origins(value, pointer)
			}
		}
		origins(&document, "")
	}
	return result, nil
}

func configPointerPart(s string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(s) }

func effectiveConfig(cfg *Config) map[string]any {
	optionalList := func(values []string) any {
		if values == nil {
			return nil
		}
		return values
	}
	permissions := "restricted"
	if cfg.AssumeDefaultPermissions == DefaultPermissionsAssumptionPermissive {
		permissions = "permissive"
	}
	var timeout any = false
	if policy := cfg.RequiresJobTimeout(); policy.Enabled() {
		bounds := map[string]any{}
		if minimum, ok := policy.MinMinutes(); ok {
			bounds["min-minutes"] = minimum
		}
		if maximum, ok := policy.MaxMinutes(); ok {
			bounds["max-minutes"] = maximum
		}
		timeout = bounds
	}
	var requiredPermissions any = false
	if policy := cfg.RequiresPermissions(); policy.Enabled() {
		requiredPermissions = map[string]any{"scope": policy.Scope()}
	}
	paths := map[string]any{}
	for pattern, path := range cfg.Paths {
		patterns := make([]string, 0, len(path.Ignore))
		for _, p := range path.Ignore {
			patterns = append(patterns, p.String())
		}
		paths[pattern] = map[string]any{"ignore": patterns}
	}
	labels := cfg.SelfHostedRunner.Labels
	if labels == nil {
		labels = []string{}
	}
	required := cfg.RequiredActions()
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"self-hosted-runner":         map[string]any{"labels": labels},
		"config-variables":           optionalList(cfg.ConfigVariables),
		"config-secrets":             optionalList(cfg.ConfigSecrets),
		"assume-default-permissions": permissions,
		"paths":                      paths,
		"policy": map[string]any{
			"require-commit-hash": cfg.RequiresCommitHash(),
			"require-job-timeout": timeout,
			"require-permissions": requiredPermissions,
			"required-actions":    required,
		},
	}
}

func runConfigCommand(out io.Writer, inv Invocation) error {
	if inv.Operation == "config path" {
		path, err := selectedConfigPath(inv.Check.Config)
		if err != nil {
			return err
		}
		if inv.JSON {
			return writeCommandJSON(out, map[string]string{"path": path})
		}
		if path != "" {
			_, err = fmt.Fprintln(out, path)
		}
		return err
	}
	inspection, err := inspectConfig(inv.Check.Config, inv.Origin)
	if err != nil {
		return err
	}
	if inv.Operation == "config validate" {
		if inv.JSON {
			return writeCommandJSON(out, struct {
				Path  string `json:"path"`
				Valid bool   `json:"valid"`
			}{inspection.Path, true})
		}
		if inspection.Path == "" {
			_, err = fmt.Fprintln(out, "No configuration file selected; defaults are valid.")
		} else {
			_, err = fmt.Fprintf(out, "Configuration is valid: %s\n", inspection.Path)
		}
		return err
	}
	if inv.JSON {
		return writeCommandJSON(out, inspection)
	}
	encoder := yaml.NewEncoder(out)
	encoder.SetIndent(2)
	if inv.Origin {
		err = encoder.Encode(inspection)
	} else {
		err = encoder.Encode(inspection.Config)
	}
	return errors.Join(err, encoder.Close())
}

func initCommandConfig(out io.Writer, inv Invocation) error {
	if inv.Check.Config.Path != "" || inv.Check.Config.Disabled {
		return commandUsageError{errors.New("config init always creates the repository config; omit --config and --no-config")}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path, err := generateProjectConfig(&Projects{skipConfig: true}, cwd)
	if err != nil {
		return err
	}
	if inv.JSON {
		return writeCommandJSON(out, map[string]string{"path": path})
	}
	_, err = fmt.Fprintf(out, "Config file was generated at %q\n", path)
	return err
}
