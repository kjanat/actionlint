package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev"
	"go.yaml.in/yaml/v4"
)

func runConfigCommand(out io.Writer, inv invocation) error {
	if inv.Operation == "config path" {
		path, err := actionlint.SelectedConfigPath(inv.Check.Config)
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
	inspection, err := actionlint.InspectConfig(inv.Check.Config, inv.Origin)
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

	if inv.Legacy && inv.Check.Config.Path != "" {
		if _, err := actionlint.ReadConfigFile(inv.Check.Config.Path); err != nil {
			return err
		}
	}
	if inv.Legacy {
		if _, err := actionlint.CompileIgnorePatterns(inv.Check.IgnoreRegex); err != nil {
			return err
		}
		options, err := resolveTemplate(inv.Render)
		if err != nil {
			return err
		}
		if options.Template != "" {
			if _, err := actionlint.NewErrorFormatter(options.Template); err != nil {
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
	path, err := actionlint.GenerateProjectConfig(cwd, !inv.Legacy)
	if err != nil {
		return err
	}
	if inv.JSON {
		return writeCommandJSON(out, map[string]string{"path": path})
	}
	_, err = fmt.Fprintf(out, "Config file was generated at %q\n", path)
	return err
}
