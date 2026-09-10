package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"slices"
	"strings"
)

// The root grammar remains Go's flag grammar, including boolean values, early
// help, and stopping at the first filename. Only its result crosses this boundary.
func (a *commandApp) parseLegacy(args []string) (rest []string, forced string, terminated bool, err error) {
	f := flag.NewFlagSet("actionlint", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.Usage = func() {}
	text := func(p *string, value string, names ...string) {
		for _, name := range names {
			f.StringVar(p, name, value, "")
		}
	}
	boolean := func(p *bool, names ...string) {
		for _, name := range names {
			f.BoolVar(p, name, false, "")
		}
	}
	c, r, o := &a.inv.Check, &a.inv.Render, &a.opts
	text(&c.ShellCheck, "shellcheck", "shellcheck")
	text(&c.Pyflakes, "pyflakes", "pyflakes")
	text(&c.Config.Path, "", "config-file", "config")
	text(&c.StdinFilename, "<stdin>", "stdin-filename")
	text(&r.Template, "", "format", "template", "f")
	text(&r.TemplateFile, "", "template-file")
	text(&r.OutputFile, "", "output-file")
	text(&o.output, "text", "output", "output-format", "o")
	text(&o.logLevel, "none", "log-level")
	text(&forced, "", "command")
	boolean(&c.Config.Disabled, "no-config")
	boolean(&r.Oneline, "oneline")
	boolean(&r.Quiet, "quiet", "q")
	boolean(&r.Summary, "summary")
	boolean(&c.Verbose, "verbose", "v")
	boolean(&c.Debug, "debug")
	boolean(&o.color, "color")
	boolean(&o.noColor, "no-color")
	boolean(&o.version, "version")
	boolean(&o.initConfig, "init-config")
	boolean(&o.helpLegacy, "help-legacy")
	boolean(&a.inv.JSON, "json")
	for _, name := range []string{"ignore", "ignore-regex"} {
		f.Func(name, "", func(s string) error { c.IgnoreRegex = append(c.IgnoreRegex, s); return nil })
	}
	f.Var(&o.completion, "completion", "")
	f.Var(&o.completion, "completions", "")
	a.errorJSON = commandRequestsJSON(a.root.Flags(), args)
	// --command selects the modern parser for everything after its value.
	// Option values that happen to contain --command or -- remain data.
	end := len(args)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			terminated = true
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			break
		}
		name, _, equals := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		v := f.Lookup(name)
		if v == nil {
			continue
		}
		if !equals {
			b, ok := v.Value.(interface{ IsBoolFlag() bool })
			if !ok || !b.IsBoolFlag() {
				i++
			}
		}
		if name == "command" {
			end = min(i+1, len(args))
			break
		}
	}
	if err = f.Parse(args[:end]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			a.inv.JSON = a.errorJSON
			a.help(a.root, nil)
			return nil, "", false, nil
		}
		return nil, "", false, commandUsageError{err}
	}
	f.Visit(func(v *flag.Flag) { a.set[v.Name] = true })
	if a.set["command"] {
		if forced == "" {
			return nil, "", false, commandUsageError{errors.New("--command requires a command name")}
		}
		return args[end:], forced, false, nil
	}
	return f.Args(), forced, terminated, nil
}

func commandFileExists(name string) bool {
	for _, candidate := range append(commandNames(), "__complete", "__completeNoDesc") {
		if name == candidate {
			info, err := os.Stat(candidate)
			return err == nil && info.Mode().IsRegular()
		}
	}
	return false
}

func isCommandName(name string) bool {
	return slices.Contains(commandNames(), name)
}

func commandNames() []string {
	return []string{"check", "config", "rules", "doctor", "completion", "version", "help"}
}
