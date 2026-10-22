package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	actionlint "actionlint.kjanat.dev/internal/engine"
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

func writeConfigValidation(ctx context.Context, out io.Writer, req configValidateRequest) error {
	inspection, err := inspectConfig(ctx, req.Config, req.Plugins, false)
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
		file := warning.File
		if file == "" {
			file = inspection.Path
		}
		if _, err := fmt.Fprintf(out, "%s:%d:%d: warning: %s\n", file, warning.Line, warning.Column, warning.Message); err != nil {
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

func writeConfigShow(ctx context.Context, out io.Writer, req configShowRequest) error {
	inspection, err := inspectConfig(ctx, req.Config, req.Plugins, req.Origin)
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
	return generateCommandConfig(streams.Stdout, req.JSON, false, req.Hyperlinks)
}

func generateCommandConfig(out io.Writer, asJSON, withSchema bool, hyperlinks hyperlinkMode) error {
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
	label, err := filepath.Rel(cwd, path)
	if err != nil {
		label = path
	}
	file, terminal := terminalFile(out)
	links := hyperlinks.enabled(terminal, os.Getenv)
	if links && terminal {
		restore := enableTerminalVT(file)
		defer restore()
	}
	_, err = fmt.Fprintf(out, "Created %s\n", fileLink(links, label))
	return err
}
