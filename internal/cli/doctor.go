package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"text/tabwriter"

	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/processconfig"
)

type doctorTool struct {
	Name      string   `json:"name"`
	Command   string   `json:"command"`
	Path      string   `json:"path,omitempty"`
	Arguments []string `json:"arguments,omitempty"`
	Status    string   `json:"status"`
	Error     string   `json:"error,omitempty"`
}

func writeDoctor(ctx context.Context, out io.Writer, req doctorRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	config, configErr := localapp.InspectConfig(ctx, req.Config, req.Plugins, false)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	report := struct {
		Build          commandBuildInfo `json:"build"`
		Directory      string           `json:"directory"`
		ConfigPath     string           `json:"config_path"`
		ConfigDisabled bool             `json:"config_disabled"`
		ConfigError    string           `json:"config_error,omitempty"`
		Tools          []doctorTool     `json:"tools"`
	}{Build: commandBuild(), Directory: cwd, ConfigPath: config.Path, ConfigDisabled: req.Config.Disabled}
	if configErr != nil {
		report.ConfigError = configErr.Error()
	}
	if req.ResolverCommand != "" {
		options := &processconfig.Options{Executable: &req.ResolverCommand, Arguments: req.ResolverArguments}
		tool := doctorTool{Name: "resolver", Command: req.ResolverCommand, Status: "available"}
		tool.Path, tool.Arguments, err = processconfig.ResolveOptions(req.ResolverCommand, options)
		if err != nil {
			tool.Status, tool.Error = "unavailable", err.Error()
		}
		report.Tools = append(report.Tools, tool)
	}
	if req.JSON {
		err = writeCommandJSON(out, report)
	} else {
		file, terminal := terminalFile(out)
		links := req.Hyperlinks.enabled(terminal, os.Getenv)
		if links && terminal {
			restore := enableTerminalVT(file)
			defer restore()
		}
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		_, err = fmt.Fprintf(w, "actionlint %s\nDirectory:\t%s\nConfiguration:\t%s\n", report.Build.Version, fileLink(links, cwd), fileLink(links, config.Path))
		if err == nil && configErr != nil {
			_, err = fmt.Fprintf(w, "Configuration error:\t%s\n", configErr)
		}
		for _, tool := range report.Tools {
			if err != nil {
				break
			}
			value := fileLink(links, tool.Path)
			if value == "" {
				value = tool.Status
			}
			if tool.Error != "" {
				value = fmt.Sprintf("%s (%s: %s)", value, tool.Status, tool.Error)
			}
			_, err = fmt.Fprintf(w, "%s:\t%s\n", tool.Name, value)
		}
		err = errors.Join(err, w.Flush())
	}
	if err != nil {
		return err
	}
	return configErr
}
