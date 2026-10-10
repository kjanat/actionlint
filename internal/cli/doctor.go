package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"actionlint.kjanat.dev"
	"actionlint.kjanat.dev/internal/ruff"
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
	}{{"shellcheck", req.ShellCheck, req.ShellcheckOptions}, {"ruff", req.Ruff, req.RuffOptions}} {
		if item.options != nil && item.options.Executable != nil {
			item.command = *item.options.Executable
		}
		tool := doctorTool{Name: item.name, Command: item.command, Status: "disabled"}
		if item.command != "" {
			tool.Path, tool.Arguments, err = actionlint.ResolveExternalCommandOptions(item.command, item.options)
			tool.Status = "available"
			if err == nil && item.name == "ruff" && item.options != nil && item.options.Optional {
				err = probeDoctorRuff(ctx, tool.Path, item.options)
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			if err != nil {
				tool.Status, tool.Error = "unavailable", err.Error()
			}
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

func probeDoctorRuff(ctx context.Context, executable string, options *actionlint.ExternalCommandOptions) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result error
	run := func(args []string, stdin string, callback func([]byte, error) error) {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.WaitDelay = time.Second
		cmd.Dir = options.WorkingDir
		cmd.Stdin = strings.NewReader(stdin)
		cmd.Env = append(cmd.Environ(), options.Environment...)
		cmd.Env = slices.DeleteFunc(cmd.Env, func(entry string) bool {
			name, _, _ := strings.Cut(entry, "=")
			return slices.ContainsFunc(ruff.UnsetEnvironment(), func(unset string) bool {
				return name == unset || runtime.GOOS == "windows" && strings.EqualFold(name, unset)
			})
		})
		output, err := cmd.Output()
		result = callback(output, err)
	}
	return ruff.CheckCompatibility(ctx, run, func() error { return result })
}
