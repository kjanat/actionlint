package actionlint

import (
	"errors"
	"flag"
	"io"
	"os"
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
	if err = f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			a.inv.JSON = a.errorJSON
			a.help(a.root, nil)
			return nil, "", false, nil
		}
		return nil, "", false, commandUsageError{err}
	}
	f.Visit(func(v *flag.Flag) { a.set[v.Name] = true })
	// A terminator used as an option value is data, so scan with the flag types.
	for i := 0; i < len(args)-len(f.Args()); i++ {
		if args[i] == "--" {
			terminated = true
			break
		}
		name, _, equals := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		v := f.Lookup(name)
		if v != nil && !equals {
			b, ok := v.Value.(interface{ IsBoolFlag() bool })
			if !ok || !b.IsBoolFlag() {
				i++
			}
		}
	}
	return f.Args(), forced, terminated, nil
}

func commandFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isCommandName(name string) bool {
	switch name {
	case "check", "config", "rules", "doctor", "completion", "version", "help":
		return true
	}
	return false
}
