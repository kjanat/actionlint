package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev"
	"go.yaml.in/yaml/v4"
)

func writeConfigPath(out io.Writer, req configPathRequest) error {
	path, err := actionlint.SelectedConfigPath(req.Config)
	if err != nil {
		return err
	}
	if req.JSON {
		return writeCommandJSON(out, map[string]string{"path": path})
	}
	if path != "" {
		_, err = fmt.Fprintln(out, path)
	}
	return err
}

func writeConfigValidation(out io.Writer, req configValidateRequest) error {
	inspection, err := actionlint.InspectConfig(req.Config, false)
	if err != nil {
		return err
	}
	if req.JSON {
		return writeCommandJSON(out, struct {
			Path     string                     `json:"path"`
			Valid    bool                       `json:"valid"`
			Warnings []actionlint.ConfigWarning `json:"warnings,omitempty"`
		}{inspection.Path, true, inspection.Warnings})
	}
	for _, warning := range inspection.Warnings {
		if _, err := fmt.Fprintf(out, "%s:%d:%d: warning: %s\n", inspection.Path, warning.Line, warning.Column, warning.Message); err != nil {
			return err
		}
	}
	if inspection.Path == "" {
		_, err = fmt.Fprintln(out, "No configuration file selected; defaults are valid.")
	} else {
		_, err = fmt.Fprintf(out, "Configuration is valid: %s\n", inspection.Path)
	}
	return err
}

func writeConfigShow(out io.Writer, req configShowRequest) error {
	inspection, err := actionlint.InspectConfig(req.Config, req.Origin)
	if err != nil {
		return err
	}
	if req.JSON {
		return writeCommandJSON(out, inspection)
	}
	encoder := yaml.NewEncoder(out)
	encoder.SetIndent(2)
	if req.Origin {
		err = encoder.Encode(inspection)
	} else {
		err = encoder.Encode(inspection.Config)
	}
	return errors.Join(err, encoder.Close())
}

func initLegacyCommandConfig(streams Command, req legacyConfigInitRequest) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if req.ConfigPath != "" {
		if _, err := actionlint.ReadConfigFile(req.ConfigPath); err != nil {
			return err
		}
	}
	if _, err := actionlint.CompileIgnorePatterns(req.IgnoreRegex); err != nil {
		return err
	}
	options, err := resolveTemplate(renderOptions{Template: req.Template, TemplateFile: req.TemplateFile})
	if err != nil {
		return err
	}
	if options.Template != "" {
		if _, err := actionlint.NewErrorFormatter(options.Template); err != nil {
			return err
		}
	}
	if req.Log {
		log := streams.Stderr
		if req.JSON {
			log = &commandJSONLogWriter{out: log}
		}
		if _, err := fmt.Fprintln(log, "verbose: Generating default actionlint.yaml in repository:", cwd); err != nil {
			return err
		}
	}
	return generateCommandConfig(streams.Stdout, req.JSON, false)
}

func generateCommandConfig(out io.Writer, asJSON, withSchema bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path, err := actionlint.GenerateProjectConfig(cwd, withSchema)
	if err != nil {
		return err
	}
	if asJSON {
		return writeCommandJSON(out, map[string]string{"path": path})
	}
	_, err = fmt.Fprintf(out, "Config file was generated at %q\n", path)
	return err
}
