package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/render"
)

func (a *commandApp) anySet(names ...string) bool {
	for _, name := range names {
		if a.set[name] {
			return true
		}
	}
	return false
}

func (a *commandApp) envString(name string, target *string, flags ...string) {
	if !a.anySet(flags...) {
		if value, ok := os.LookupEnv(name); ok {
			*target = value
		}
	}
}

func (a *commandApp) envBool(name string, target *bool, flags ...string) error {
	if a.anySet(flags...) {
		return nil
	}
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}
	if value == "" {
		*target = false
		return nil
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return commandUsageError{fmt.Errorf("%s must be true or false", name)}
	}
	*target = b
	return nil
}

// Apply defaults after parsing so either grammar can explicitly override them.
// Only the selected operation reads its settings; completion never reads them.
func (a *commandApp) applyEnvironment() error {
	if a.inv.Operation == operationCompletion {
		return nil
	}
	if err := a.presentationEnvironment(); err != nil {
		return err
	}
	i := &a.inv
	if err := a.envBool("ACTIONLINT_QUIET", &i.Render.Quiet, "quiet", "q"); err != nil {
		return err
	}
	switch i.Operation {
	case "lsp", operationCheck, operationRules, operationDoctor, operationConfigPath, operationConfigShow, operationConfigValidate, operationConfigMigrate,
		operationPluginInstall, operationPluginUpdate, operationPluginRemove, operationPluginList, operationPluginSync:
		if !a.anySet("config", "config-file", "no-config") {
			a.envString("ACTIONLINT_CONFIG", &i.Check.Config.Path)
			if err := a.envBool("ACTIONLINT_NO_CONFIG", &i.Check.Config.Disabled); err != nil {
				return err
			}
		}
	}
	if i.Operation == operationConfigShow {
		return a.envBool("ACTIONLINT_CONFIG_ORIGIN", &i.Origin, "origin")
	}
	if i.Operation != operationCheck && i.Operation != operationDoctor && i.Operation != "lsp" {
		return nil
	}
	if i.Operation == operationDoctor {
		return nil
	}
	return a.checkEnvironment()
}

func (a *commandApp) presentationEnvironment() error {
	if err := a.envBool("ACTIONLINT_JSON_PRETTY", &a.inv.Render.PrettyJSON, "json-pretty"); err != nil {
		return err
	}
	a.inv.Render.PrettyJSONExplicit = a.set["json-pretty"]
	if !a.formatSelected() && !a.prettyJSONSelected() {
		if err := a.envBool("ACTIONLINT_JSON", &a.inv.JSON); err != nil {
			return err
		}
	}
	if !a.anySet("color", "modern-color", "no-color") {
		if mode := os.Getenv("ACTIONLINT_COLOR"); mode != "" {
			color, err := parseColorMode(mode)
			if err != nil {
				return commandUsageError{fmt.Errorf("ACTIONLINT_COLOR: %w", err)}
			}
			if os.Getenv("NO_COLOR") != "" {
				color = render.ColorNever
			}
			a.inv.Render.Color = color
		}
	}
	if !a.set["hyperlinks"] {
		if mode := os.Getenv("ACTIONLINT_HYPERLINKS"); mode != "" {
			if err := a.inv.Render.Hyperlinks.Set(mode); err != nil {
				return commandUsageError{fmt.Errorf("ACTIONLINT_HYPERLINKS: %w", err)}
			}
			if os.Getenv("NO_HYPERLINKS") != "" {
				a.inv.Render.Hyperlinks = "never"
			}
		}
	}
	return nil
}

func (a *commandApp) checkEnvironment() error {
	i := &a.inv
	if err := a.envBool("ACTIONLINT_STRICT", &i.Check.Strict, "strict"); err != nil {
		return err
	}
	if err := a.envBool("ACTIONLINT_EXPERIMENTAL", &i.Check.Experimental, "experimental"); err != nil {
		return err
	}
	_, experimentalEnv := os.LookupEnv("ACTIONLINT_EXPERIMENTAL")
	i.Check.ExperimentalSet = a.set["experimental"] || experimentalEnv
	if !a.formatSelected() && !a.prettyJSONSelected() {
		if value := os.Getenv("ACTIONLINT_OUTPUT_FORMAT"); value != "" {
			a.opts.output, a.set["output-format"] = value, true
		}
		a.envString("ACTIONLINT_TEMPLATE", &i.Render.Template)
		a.envString("ACTIONLINT_TEMPLATE_FILE", &i.Render.TemplateFile)
	}
	a.envString("ACTIONLINT_OUTPUT_FILE", &i.Render.OutputFile, "output-file")
	a.envString("ACTIONLINT_STDIN_FILENAME", &i.Check.StdinFilename, "stdin-filename")
	if err := a.envBool("ACTIONLINT_SUMMARY", &i.Render.Summary, "summary"); err != nil {
		return err
	}
	if !a.anySet("log-level", "verbose", "v", "debug") {
		if value := os.Getenv("ACTIONLINT_LOG_LEVEL"); value != "" {
			a.opts.logLevel, a.set["log-level"] = value, true
		}
	}
	if !a.anySet("ignore-regex", "ignore") {
		if value := os.Getenv("ACTIONLINT_IGNORE_REGEX"); value != "" {
			patterns, err := environmentStringArray(value)
			if err != nil {
				return commandUsageError{errors.New("ACTIONLINT_IGNORE_REGEX must be a JSON array of strings")}
			}
			i.Check.IgnoreRegex = patterns
		}
	}
	return nil
}
