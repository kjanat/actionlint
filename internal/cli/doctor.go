package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"text/tabwriter"
	"time"

	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
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
	tools := []struct {
		name, command string
		options       *processconfig.Options
	}{
		{"shellcheck", req.ShellCheck, req.ShellcheckOptions},
		{"ruff", req.Ruff, req.RuffOptions},
		{"resolver", req.ResolverCommand, &processconfig.Options{Executable: &req.ResolverCommand, Arguments: req.ResolverArguments}},
	}
	for _, item := range tools {
		if item.name == "resolver" && item.command == "" {
			continue
		}
		if item.options != nil && item.options.Executable != nil {
			item.command = *item.options.Executable
		}
		tool := doctorTool{Name: item.name, Command: item.command, Status: "disabled"}
		if item.command != "" {
			tool.Path, tool.Arguments, err = processconfig.ResolveOptions(item.command, item.options)
			tool.Status = "available"
			if err == nil && item.options != nil && item.options.Optional {
				err = probeDoctorVersion(ctx, item.name, item.command, item.options)
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

// probeDoctorVersion runs the version probe of the bundled provider named
// name against command, as analysis does before it uses an automatically
// discovered tool. A provider without a version probe passes.
func probeDoctorVersion(ctx context.Context, name, command string, options *processconfig.Options) error {
	manifests, err := provider.Bundled()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(manifests, func(m provider.Manifest) bool { return m.Rule.Name == name })
	if index < 0 || manifests[index].Process == nil || manifests[index].Process.Version == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	processes := analyzerhost.NewProcessPool(ctx, 1)
	defer processes.Wait()
	resolved, err := processes.ConfiguredCommandRunner(command, options, false)
	if err != nil {
		return err
	}
	backend, err := process.NewBackend(manifests[index], resolved, process.Options{Programs: bundled.Program, Required: true})
	if err != nil {
		return err
	}
	_, err = backend.ProbeVersion(ctx)
	return err
}
