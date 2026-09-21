package githubaction

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev"
	"github.com/spf13/pflag"
)

type shellcheckFlags struct {
	settings actionlint.ShellcheckSettings
	order    []string
	values   map[string][]string
}

func (f *shellcheckFlags) set(name, value string, accumulate bool) {
	if _, exists := f.values[name]; !exists {
		f.order = append(f.order, name)
	}
	if !accumulate {
		f.values[name] = []string{value}
	} else if !slices.Contains(f.values[name], value) {
		f.values[name] = append(f.values[name], value)
	}
}

func (f *shellcheckFlags) arguments() []string {
	var args []string
	for _, name := range f.order {
		if name == "exclude" {
			continue // Combined with actionlint's built-in exclusions by the rule.
		}
		if name == "include" || name == "enable" {
			args = append(args, "--"+name, strings.Join(f.values[name], ","))
			continue
		}
		for _, value := range f.values[name] {
			args = append(args, "--"+name, value)
		}
	}
	return args
}

func mergeShellcheckFlags(inherited, explicit []string) (*shellcheckFlags, error) {
	merged := &shellcheckFlags{values: map[string][]string{}}
	flags := pflag.NewFlagSet("ShellCheck", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVarP(&merged.settings.Shell, "shell", "s", "", "")
	flags.Func("rcfile", "", func(value string) error {
		merged.settings.Config = actionlint.ShellcheckRCFile(value)
		return nil
	})
	flags.BoolFunc("norc", "", func(value string) error {
		if value != "true" {
			return errors.New("use shellcheck-config: true to enable discovery")
		}
		merged.settings.Config = actionlint.ShellcheckRCDisabled
		return nil
	})
	for _, option := range []struct{ name, short string }{
		{"exclude", "e"}, {"include", "i"}, {"enable", "o"},
	} {
		flags.FuncP(option.name, option.short, "", func(value string) error {
			for item := range strings.SplitSeq(value, ",") {
				if item == "" {
					continue
				}
				if option.name != "enable" {
					code, err := strconv.Atoi(strings.TrimPrefix(item, "SC"))
					if err != nil || code < 0 {
						return fmt.Errorf("invalid ShellCheck code %q", item)
					}
					item = "SC" + strconv.Itoa(code)
				}
				merged.set(option.name, item, true)
			}
			return nil
		})
	}
	flags.FuncP("source-path", "P", "", func(value string) error {
		merged.set("source-path", value, true)
		return nil
	})
	for _, option := range []struct{ name, short string }{
		{"severity", "S"}, {"extended-analysis", ""},
	} {
		flags.FuncP(option.name, option.short, "", func(value string) error {
			if option.name == "severity" && !slices.Contains([]string{"error", "warning", "info", "style"}, value) {
				return errors.New("severity must be error, warning, info or style")
			}
			if option.name == "extended-analysis" && value != "true" && value != "false" {
				return errors.New("extended-analysis must be true or false")
			}
			merged.set(option.name, value, false)
			return nil
		})
	}
	// These flags are accepted but normalized to the rule's JSON1 transport.
	flags.StringP("format", "f", "json1", "")
	flags.StringP("color", "C", "auto", "")
	flags.Lookup("color").NoOptDefVal = "always"
	flags.IntP("wiki-link-count", "W", 0, "")
	externalSources := flags.BoolP("external-sources", "x", true, "")
	for _, option := range []struct{ name, short string }{
		{"version", "V"}, {"help", ""}, {"list-optional", ""},
	} {
		flags.BoolFuncP(option.name, option.short, "", func(string) error {
			return fmt.Errorf("--%s exits without checking the workflow", option.name)
		})
	}
	flags.BoolFuncP("check-sourced", "a", "", func(string) error {
		return errors.New("findings in sourced files cannot yet be mapped to workflow locations")
	})
	flags.Func("files-from", "", func(string) error {
		return errors.New("additional input files cannot be mapped to workflow locations")
	})
	for _, source := range []struct {
		name string
		args []string
	}{{"SHELLCHECK_OPTS", inherited}, {"shellcheck-args", explicit}} {
		for _, arg := range source.args {
			if strings.ContainsRune(arg, 0) {
				return nil, inputErrorf("%s: arguments must not contain NUL", source.name)
			}
		}
		if err := flags.Parse(source.args); err != nil {
			return nil, inputErrorf("%s: %s", source.name, err)
		}
		if flags.NArg() > 0 {
			return nil, inputErrorf("%s: additional input files cannot be mapped to workflow locations", source.name)
		}
	}
	merged.settings.Exclude = merged.values["exclude"]
	if flags.Changed("external-sources") {
		merged.settings.ExternalSources = externalSources
	}
	return merged, nil
}
