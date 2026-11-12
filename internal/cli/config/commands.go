package configcmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	"actionlint.kjanat.dev/v2/internal/input/configuration/schema"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	"github.com/kjanat/go-yaml/v4"
)

type PathOptions struct {
	Config repoconfig.Selection
	JSON   bool
}

type ShowOptions struct {
	Config       repoconfig.Selection
	Plugins      []string
	JSON, Origin bool
}

type ValidateOptions struct {
	Config  repoconfig.Selection
	Plugins []string
	JSON    bool
}

type SchemaOptions struct {
	Config  repoconfig.Selection
	Plugins []string
}

type MigrateOptions struct {
	Config             repoconfig.Selection
	JSON, Write, Check bool
}

func WritePath(out io.Writer, req PathOptions) error {
	path, err := repoconfig.SelectedPath(req.Config)
	if err != nil {
		return err
	}
	if req.JSON {
		return commandstreams.WriteJSON(out, map[string]string{"path": path})
	}
	if path != "" {
		_, err = fmt.Fprintln(out, path)
	}
	return err
}

func WriteValidation(ctx context.Context, out io.Writer, req ValidateOptions, acquisition plugininstall.Acquisition) error {
	inspection, err := checking.InspectConfig(ctx, req.Config, req.Plugins, false, acquisition)
	if err != nil {
		return err
	}
	if req.JSON {
		return commandstreams.WriteJSON(out, struct {
			Path     string                 `json:"path"`
			Valid    bool                   `json:"valid"`
			Warnings []configreport.Warning `json:"warnings,omitempty"`
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

// WriteSchema writes the configuration JSON schema to out. Beyond the
// published actionlint.schema.json, it describes tools.<name> for every tool
// provider that the --plugin flags or the selected configuration install.
func WriteSchema(ctx context.Context, out io.Writer, req SchemaOptions, acquisition plugininstall.Acquisition) error {
	manifests, err := checking.InstalledProviders(ctx, req.Config, req.Plugins, acquisition)
	if err != nil {
		return err
	}
	document, err := schema.Document(schema.Schema, manifests)
	if err != nil {
		return err
	}
	_, err = out.Write(document)
	return err
}

func WriteShow(ctx context.Context, out io.Writer, req ShowOptions, acquisition plugininstall.Acquisition) error {
	inspection, err := checking.InspectConfig(ctx, req.Config, req.Plugins, req.Origin, acquisition)
	if err != nil {
		return err
	}
	if req.JSON {
		return commandstreams.WriteJSON(out, inspection)
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

func Migrate(ctx context.Context, streams commandstreams.Streams, req MigrateOptions, acquisition plugininstall.Acquisition) (int, error) {
	path, err := repoconfig.SelectedPath(req.Config)
	if err != nil {
		return 0, err
	}
	if path == "" {
		return 0, errors.New("config migrate requires a selected config file; use --config PATH")
	}
	migration, err := plugininstall.PrepareConfigurationMigration(ctx, path, plugininstall.DefaultPluginOptions{
		RuleSet:     checking.ConfigurationRules(),
		Tools:       registry.IDs(),
		Warning:     func(message string) { _, _ = fmt.Fprintln(streams.Stderr, "warning: "+message) },
		Acquisition: acquisition,
	})
	if err != nil {
		return 0, err
	}
	if req.Write {
		if err := migration.Apply(ctx); err != nil {
			return 0, err
		}
	}
	if req.JSON {
		err = commandstreams.WriteJSON(streams.Stdout, struct {
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
