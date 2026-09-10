package actionlint

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

func selectedConfigPath(selection configSelection) (string, error) {
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

func inspectConfig(selection configSelection, withOrigin bool) (configInspection, error) {
	path, err := selectedConfigPath(selection)
	if err != nil {
		return configInspection{}, err
	}
	cfg := &Config{}
	document := &yaml.Node{}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return configInspection{Path: path}, fmt.Errorf("could not read config file %q: %w", path, err)
		}
		cfg, document, err = parseConfigDocument(data)
		if err != nil {
			return configInspection{Path: path}, fmt.Errorf("could not parse config file %q: %w", path, err)
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
		if err := configOrigins(document, "", result.Origins); err != nil {
			return configInspection{}, err
		}
	}
	return result, nil
}

func runConfigCommand(out io.Writer, inv invocation) error {
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

func initCommandConfig(streams Command, inv invocation) error {
	out := streams.Stdout
	if !inv.Legacy && (inv.Check.Config.Path != "" || inv.Check.Config.Disabled) {
		return commandUsageError{errors.New("config init always creates the repository config; omit --config and --no-config")}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	projects := &Projects{skipConfig: !inv.Legacy}
	if inv.Legacy && inv.Check.Config.Path != "" {
		if _, err := ReadConfigFile(inv.Check.Config.Path); err != nil {
			return err
		}
	}
	if inv.Legacy {
		if _, err := compileIgnorePatterns(inv.Check.IgnoreRegex); err != nil {
			return err
		}
		options, err := resolveTemplate(inv.Render)
		if err != nil {
			return err
		}
		if options.Template != "" {
			if _, err := NewErrorFormatter(options.Template); err != nil {
				return err
			}
		}
		if (inv.Check.Verbose || inv.Check.Debug) && !inv.Render.Quiet {
			log := streams.Stderr
			if inv.JSON {
				log = &commandJSONLogWriter{out: log}
			}
			if _, err := fmt.Fprintln(log, "verbose: Generating default actionlint.yaml in repository:", cwd); err != nil {
				return err
			}
		}
	}
	path, err := generateProjectConfig(projects, cwd)
	if err != nil {
		return err
	}
	if inv.JSON {
		return writeCommandJSON(out, map[string]string{"path": path})
	}
	_, err = fmt.Fprintf(out, "Config file was generated at %q\n", path)
	return err
}
