package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	configsource "actionlint.kjanat.dev/v2/internal/input/configuration/source"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	"actionlint.kjanat.dev/v2/internal/input/discovery"
	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	render "actionlint.kjanat.dev/v2/internal/output"
)

func initLegacyCommandConfig(ctx context.Context, streams Command, req legacyConfigInitRequest) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if req.ConfigPath != "" {
		reader := configsource.New(ctx, nil, streams.Acquisition.Connections)
		if _, err := checking.ConfigurationRules().ReadContext(ctx, req.ConfigPath, reader.ReadFile); err != nil {
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
	return generateCommandConfig(ctx, streams.Stdout, req.JSON, req.Hyperlinks, repoconfig.FormatYAML, streams.Acquisition)
}

func generateCommandConfig(ctx context.Context, out io.Writer, asJSON bool, hyperlinks hyperlinkMode, format repoconfig.FileFormat, acquisition plugininstall.Acquisition) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path, err := plugininstall.InitializeConfiguration(ctx, checking.ConfigurationRules(), acquisition, discovery.RepositoryRoot(cwd), buildinfo.Version(), format)
	if err != nil {
		return err
	}
	if asJSON {
		return commandstreams.WriteJSON(out, map[string]string{"path": path})
	}
	// Resolve checkout aliases before comparing with plugin storage paths.
	physicalCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return err
	}
	label, err := filepath.Rel(physicalCwd, path)
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
