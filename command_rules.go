package actionlint

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
)

func commandRules() []ruleDescriptor {
	rules := builtinRuleDescriptors()
	slices.SortFunc(rules, func(a, b ruleDescriptor) int { return strings.Compare(a.Name, b.Name) })
	return rules
}

func writeRules(out io.Writer, name string, asJSON bool) error {
	rules := commandRules()
	if name != "" {
		for _, rule := range rules {
			if rule.Name == name {
				if asJSON {
					return writeCommandJSON(out, rule)
				}
				_, err := fmt.Fprintf(out, "%s (%s)\n\n%s\n", rule.Name, rule.Category, rule.Description)
				return err
			}
		}
		return commandUsageError{fmt.Errorf("unknown rule %q; run actionlint rules to list checks", name)}
	}
	if asJSON {
		return writeCommandJSON(out, rules)
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	for _, rule := range rules {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", rule.Name, rule.Category, rule.Description); err != nil {
			return err
		}
	}
	return w.Flush()
}

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
	config, configErr := inspectConfig(req.Config, false)
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
			tool.Path, tool.Arguments, err = resolveExternalCommand(item.command)
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
