package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"actionlint.kjanat.dev"
)

type doctorTool struct {
	Name      string   `json:"name"`
	Command   string   `json:"command"`
	Path      string   `json:"path,omitempty"`
	Arguments []string `json:"arguments,omitempty"`
	Status    string   `json:"status"`
	Error     string   `json:"error,omitempty"`
}

func writeDoctor(out io.Writer, req checkInvocation, asJSON bool, mode hyperlinkMode) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	config, configErr := actionlint.InspectConfig(req.Config, false)
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
	for _, item := range []struct {
		name, command string
		options       *actionlint.ExternalCommandOptions
	}{{"shellcheck", req.ShellCheck, req.ShellcheckOptions}, {"pyflakes", req.Pyflakes, req.PyflakesOptions}} {
		if item.options != nil && item.options.Executable != nil {
			item.command = *item.options.Executable
		}
		tool := doctorTool{Name: item.name, Command: item.command, Status: "disabled"}
		if item.command != "" {
			tool.Path, tool.Arguments, err = actionlint.ResolveExternalCommandOptions(item.command, item.options)
			tool.Status = "available"
			if err != nil {
				tool.Status, tool.Error = "unavailable", err.Error()
			}
		}
		report.Tools = append(report.Tools, tool)
	}
	if asJSON {
		err = writeCommandJSON(out, report)
	} else {
		file, terminal := terminalFile(out)
		links := mode.enabled(terminal, os.Getenv)
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
			_, err = fmt.Fprintf(w, "%s:\t%s\n", tool.Name, value)
		}
		err = errors.Join(err, w.Flush())
	}
	if err != nil {
		return err
	}
	return configErr
}
