package cli

import (
	"fmt"
	"io"
	"os"

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

func writeDoctor(out io.Writer, req checkInvocation, asJSON bool) error {
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
	for _, item := range []struct{ name, command string }{{"shellcheck", req.ShellCheck}, {"pyflakes", req.Pyflakes}} {
		tool := doctorTool{Name: item.name, Command: item.command, Status: "disabled"}
		if item.command != "" {
			tool.Path, tool.Arguments, err = actionlint.ResolveExternalCommand(item.command)
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
		_, err = fmt.Fprintf(out, "actionlint %s\nDirectory: %s\nConfiguration: %s\n", report.Build.Version, cwd, config.Path)
		if err == nil && configErr != nil {
			_, err = fmt.Fprintln(out, "Configuration error:", configErr)
		}
		for _, tool := range report.Tools {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(out, "%s: %s %s\n", tool.Name, tool.Status, tool.Path)
		}
	}
	if err != nil {
		return err
	}
	return configErr
}
