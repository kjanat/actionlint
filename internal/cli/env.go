package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev"
	"github.com/mattn/go-shellwords"
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
	case operationCheck, operationDoctor, operationConfigPath, operationConfigShow, operationConfigValidate:
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
	if i.Operation != operationCheck && i.Operation != operationDoctor {
		return nil
	}
	var err error
	if !a.set["shellcheck"] {
		i.Check.ShellcheckOptions, err = externalToolEnvironment("SHELLCHECK")
		if err != nil {
			return err
		}
	}
	if !a.set["pyflakes"] {
		i.Check.PyflakesOptions, err = externalToolEnvironment("PYFLAKES")
		if err != nil {
			return err
		}
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
	if !a.anySet("json", "output-format", "output", "o", "template", "format", "f", "template-file", "oneline") {
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
				color = actionlint.ColorOptionKindNever
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
	if !a.anySet("output-format", "output", "o", "json", "template", "format", "f", "template-file", "oneline") {
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

func externalToolEnvironment(tool string) (*actionlint.ExternalCommandOptions, error) {
	prefix := "ACTIONLINT_" + tool
	options := &actionlint.ExternalCommandOptions{}
	if bin, ok := os.LookupEnv(prefix + "_BIN"); ok {
		if strings.ContainsRune(bin, 0) {
			return nil, commandUsageError{fmt.Errorf("%s_BIN cannot contain NUL", prefix)}
		}
		options.Executable = &bin
		if bin == "" {
			return options, nil
		}
	}
	if flags := os.Getenv(prefix + "_FLAGS"); flags != "" {
		var err error
		options.Arguments, err = parseEnvironmentWords(flags)
		if err != nil {
			return nil, commandUsageError{fmt.Errorf("%s_FLAGS must be a JSON string array or a quoted argument list", prefix)}
		}
	}
	if value := os.Getenv(prefix + "_ENV"); value != "" {
		var err error
		options.Environment, err = parseChildEnvironment(value)
		if err != nil {
			return nil, commandUsageError{fmt.Errorf("%s_ENV: %w", prefix, err)}
		}
	}
	if options.Executable == nil && len(options.Arguments) == 0 && len(options.Environment) == 0 {
		return nil, nil
	}
	return options, nil
}

func parseEnvironmentWords(value string) ([]string, error) {
	var words []string
	if strings.HasPrefix(strings.TrimSpace(value), "[") {
		var err error
		words, err = environmentStringArray(value)
		if err != nil {
			return nil, err
		}
	} else {
		parser := shellwords.NewParser()
		parser.ParseEnv, parser.ParseBacktick = false, false
		var err error
		words, err = parser.Parse(value)
		if err != nil {
			return nil, err
		}
		if parser.Position >= 0 {
			return nil, errors.New("shell operators are not supported")
		}
	}
	for _, word := range words {
		if strings.ContainsRune(word, 0) {
			return nil, errors.New("arguments cannot contain NUL")
		}
	}
	return words, nil
}

func environmentStringArray(value string) ([]string, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(value), &entries); err != nil || entries == nil {
		return nil, errors.New("expected a JSON string array")
	}
	words := make([]string, len(entries))
	for i, entry := range entries {
		if len(entry) == 0 || entry[0] != '"' {
			return nil, errors.New("expected a JSON string")
		}
		if err := json.Unmarshal(entry, &words[i]); err != nil {
			return nil, err
		}
	}
	return words, nil
}

func parseChildEnvironment(value string) ([]string, error) {
	var entries []string
	if strings.HasPrefix(strings.TrimSpace(value), "{") {
		var object map[string]*string
		if err := json.Unmarshal([]byte(value), &object); err != nil {
			return nil, errors.New("expected a JSON object with string or null values")
		}
		for key, value := range object {
			if !validEnvironmentName(key) {
				return nil, errors.New("variable names must match [A-Za-z_][A-Za-z0-9_]*")
			}
			entry := key
			if value != nil {
				entry += "=" + *value
			}
			entries = append(entries, entry)
		}
		slices.Sort(entries)
	} else {
		var err error
		entries, err = parseEnvironmentWords(value)
		if err != nil {
			return nil, errors.New("expected variable names or NAME=value entries")
		}
	}
	var environment []string
	for _, entry := range entries {
		name, _, set := strings.Cut(entry, "=")
		if !validEnvironmentName(name) || strings.ContainsRune(entry, 0) {
			return nil, errors.New("variable names must match [A-Za-z_][A-Za-z0-9_]* and values cannot contain NUL")
		}
		if !set {
			value, exists := os.LookupEnv(name)
			if !exists {
				continue
			}
			entry += "=" + value
		}
		environment = append(environment, entry)
	}
	return environment, nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}
