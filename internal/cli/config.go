package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/localapp"

	"actionlint.kjanat.dev/internal/buildinfo"
	"actionlint.kjanat.dev/internal/projectpath"
	"actionlint.kjanat.dev/internal/render"
	"go.yaml.in/yaml/v4"
)

func writeConfigPath(out io.Writer, req configPathRequest) error {
	path, err := repoconfig.SelectedPath(req.Config)
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
	inspection, err := repoconfig.Inspect(req.Config, false)
	if err != nil {
		return err
	}
	if req.JSON {
		return writeCommandJSON(out, struct {
			Path     string               `json:"path"`
			Valid    bool                 `json:"valid"`
			Warnings []repoconfig.Warning `json:"warnings,omitempty"`
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

func writeConfigShow(out io.Writer, req configShowRequest) error {
	inspection, err := repoconfig.Inspect(req.Config, req.Origin)
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

func migrateCommandConfig(ctx context.Context, streams Command, req configMigrateRequest) (int, error) {
	path, err := repoconfig.SelectedPath(req.Config)
	if err != nil {
		return 0, err
	}
	if path == "" {
		return 0, errors.New("config migrate requires a selected config file; use --config PATH")
	}
	migration, err := localapp.PrepareConfigMigration(ctx, path, localapp.DefaultPluginOptions{Shellcheck: true, Ruff: true,
		Warning: func(message string) { _, _ = fmt.Fprintln(streams.Stderr, "warning: "+message) }})
	if err != nil {
		return 0, err
	}
	if req.Write {
		if err := migration.Apply(ctx); err != nil {
			return 0, err
		}
	}
	if req.JSON {
		err = writeCommandJSON(streams.Stdout, struct {
			Path    string   `json:"path"`
			Changed bool     `json:"changed"`
			Content string   `json:"content"`
			Notes   []string `json:"notes,omitempty"`
		}{migration.Path, migration.Changed, string(migration.Content), migration.Notes})
	} else {
		for _, note := range migration.Notes {
			if _, err := fmt.Fprintln(streams.Stderr, note); err != nil {
				return 0, err
			}
		}
		if !req.Check && !req.Write {
			_, err = streams.Stdout.Write(migration.Content)
		} else {
			_, err = fmt.Fprintf(streams.Stdout, "%s: migration needed=%t written=%t\n", path, migration.Changed, req.Write && migration.Changed)
		}
	}
	if req.Check && migration.Changed {
		return 1, err
	}
	return 0, err
}

func initLegacyCommandConfig(ctx context.Context, streams Command, req legacyConfigInitRequest) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if req.ConfigPath != "" {
		if _, err := repoconfig.Load(req.ConfigPath); err != nil {
			return err
		}
	}
	if _, err := repoconfig.CompileIgnorePatterns(req.IgnoreRegex); err != nil {
		return err
	}
	options, err := resolveTemplate(renderOptions{Template: req.Template, TemplateFile: req.TemplateFile})
	if err != nil {
		return err
	}
	if options.Template != "" {
		if _, err := render.NewFormatter(options.Template, buildinfo.Version, nil); err != nil {
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
	return generateCommandConfig(ctx, streams.Stdout, req.JSON, req.Hyperlinks)
}

func generateCommandConfig(ctx context.Context, out io.Writer, asJSON bool, hyperlinks hyperlinkMode) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path, err := localapp.InitializeConfig(ctx, projectpath.RepositoryRoot(cwd), buildinfo.Version())
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
