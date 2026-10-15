package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strings"

	"actionlint.kjanat.dev"
	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var commandHelpGroups = []string{"Input", "Configuration", "Rule selection", "Filtering", "Output", "External linters", "Information"}

var helpFlagPattern = regexp.MustCompile(`(?m)(^|[ \t])--?(?:\[no-\])?[a-zA-Z][a-zA-Z0-9-]*`)

func (a *commandApp) helpOutput(c *cobra.Command) (io.Writer, bool, func()) {
	out := a.streams.Stderr
	file, terminal := terminalFile(out)
	enabled := a.helpColor(c, terminal)
	links := a.helpHyperlinks()
	restore := func() {}
	if terminal && (enabled || links) {
		restore = enableTerminalVT(file)
	}
	// The Windows color fallback does not pass OSC 8 through.
	if terminal && enabled && !links {
		out = colorable.NewColorable(file)
	}
	return out, enabled, restore
}

func (a *commandApp) helpColor(c *cobra.Command, terminal bool) bool {
	if a.opts.noColor || a.jsonOutput() {
		return false
	}
	force := a.opts.color
	if !a.anySet("color", "modern-color", "no-color") {
		switch a.inv.Render.Color {
		case actionlint.ColorOptionKindAlways:
			force = true
		case actionlint.ColorOptionKindNever:
			return false
		}
	}
	// Help runs before capture and prepareInvocation, but its flags are parsed.
	if c != a.root && c.Flags().Changed("color") {
		switch a.opts.colorMode {
		case "always":
			force = true
		case "never":
			return false
		case "auto":
			force = false
		}
	}
	return force || githubActionsColor(a.streams.Stderr) || (terminal && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb")
}

func helpStyle(enabled bool, attributes ...color.Attribute) *color.Color {
	style := color.New(attributes...)
	if enabled {
		style.EnableColor()
	} else {
		style.DisableColor()
	}
	return style
}

func styleHelpFlags(text string, style *color.Color) string {
	return helpFlagPattern.ReplaceAllStringFunc(text, func(match string) string {
		start := strings.IndexByte(match, '-')
		return match[:start] + style.Sprint(match[start:])
	})
}

func (a *commandApp) help(c *cobra.Command, _ []string) {
	a.helpShown = true
	c.Flags().Visit(func(f *pflag.Flag) { a.set[f.Name] = true })
	if err := a.presentationEnvironment(); err != nil {
		a.status = a.reportError(err)
		return
	}
	c.InitDefaultHelpFlag()
	annotateFlag(c.Flags(), "help", "Information")
	if a.jsonOutput() {
		if c != a.root && c.Flags().Changed("color") {
			a.set["modern-color"] = true
		}
		if err := a.prepareColor(); err != nil {
			a.status = a.reportError(err)
			return
		}
		out := a.streams.Stdout
		if a.inv.Render.PrettyJSON {
			out = &prettyJSONOutput{Writer: out, ctx: c.Context(), color: a.inv.Render.Color, explicit: a.inv.Render.PrettyJSONExplicit}
		}
		if err := writeCommandJSON(out, describeCommand(c)); err != nil {
			a.status = a.reportError(err)
		}
		return
	}
	out, colored, restore := a.helpOutput(c)
	defer restore()
	width := 120
	if file, ok := a.streams.Stderr.(*os.File); ok {
		if columns := terminalWidth(file); columns > 0 {
			width = columns
		}
	}
	links := a.helpHyperlinks()
	heading := helpStyle(colored, color.Bold, color.FgCyan)
	command := helpStyle(colored, color.Bold)
	option := helpStyle(colored, color.FgCyan)
	if c == a.root {
		a.rootHelp(out, width, colored, links)
		return
	}
	usage := strings.Replace(c.UseLine(), "actionlint", terminalLink(links, "actionlint", projectURL), 1)
	_, _ = fmt.Fprintln(out, command.Sprintf("actionlint %s", actionlint.Version()))
	_, _ = fmt.Fprintf(out, "%s\n\n%s\n  %s\n", command.Sprint(c.Short), heading.Sprint("Usage:"), command.Sprint(usage))
	if c.Long != "" {
		_, _ = fmt.Fprintf(out, "\n%s\n", c.Long)
	}
	if c.Example != "" {
		_, _ = fmt.Fprintf(out, "\n%s\n%s\n", heading.Sprint("Examples:"), c.Example)
	}
	if c.HasAvailableSubCommands() {
		_, _ = fmt.Fprintln(out, "\n"+heading.Sprint("Commands:"))
		for _, sub := range c.Commands() {
			if !sub.Hidden && sub.Name() != "help" {
				_, _ = fmt.Fprintf(out, "  %s %s\n", command.Sprintf("%-12s", sub.Name()), sub.Short)
			}
		}
	}
	for _, group := range commandHelpGroups {
		fs := pflag.NewFlagSet(group, pflag.ContinueOnError)
		flags := pflag.NewFlagSet("help", pflag.ContinueOnError)
		flags.AddFlagSet(c.Flags())
		flags.AddFlagSet(c.InheritedFlags())
		flags.VisitAll(func(f *pflag.Flag) {
			if name, negated := strings.CutPrefix(f.Name, "no-"); negated {
				if positive := flags.Lookup(name); positive != nil && len(positive.Annotations[commandOptionalValueAnnotation]) > 0 {
					return
				}
			}
			if groups := f.Annotations[commandGroupAnnotation]; len(groups) > 0 && groups[0] == group {
				display := *f
				if value := f.Annotations[commandOptionalValueAnnotation]; len(value) > 0 {
					display.Name += "[=" + value[0] + "]"
					if flags.Lookup("no-"+f.Name) != nil {
						display.Name = "[no-]" + display.Name
					}
					display.NoOptDefVal, display.DefValue = "", ""
					display.Value = helpOptionalValue{Value: f.Value}
				}
				fs.AddFlag(&display)
			}
		})
		if usage := fs.FlagUsagesWrapped(width); usage != "" {
			_, _ = fmt.Fprintf(out, "\n%s\n%s", heading.Sprint(group+":"), styleHelpFlags(usage, option))
		}
	}
	_, _ = fmt.Fprintf(out, "\n%s\n  0  No findings   1  Findings   2  Invalid arguments   3  Could not complete\n", heading.Sprint("Exit status:"))
	writeHelpDestinations(out, heading, links)
}

// Root help is an overview. The registered flags remain available to the parser,
// completions and JSON help, while check --help lists all checking options.
func (a *commandApp) rootHelp(out io.Writer, width int, colored, links bool) {
	heading := helpStyle(colored, color.Bold, color.FgCyan)
	command := helpStyle(colored, color.Bold)
	option := helpStyle(colored, color.FgCyan)
	name := terminalLink(links, "actionlint", projectURL)
	_, _ = fmt.Fprintln(out, command.Sprintf("actionlint %s — Check workflows, actions and embedded scripts", actionlint.Version()))
	_, _ = fmt.Fprintf(out, "\n%s\n  %s [flags] [files...]\n  %s [flags] -\n  %s <command>\n", heading.Sprint("Usage:"), command.Sprint(name), command.Sprint(name), command.Sprint(name))
	_, _ = fmt.Fprintln(out, "\nNo files: discover workflows and actions. Use - for stdin.\nUse -- before filenames beginning with a dash.")
	_, _ = fmt.Fprintln(out, "\n"+heading.Sprint("Commands:"))
	for _, name := range []string{"check", "config", "rules", "doctor", "completion", "version"} {
		for _, sub := range a.root.Commands() {
			if sub.Name() == name {
				_, _ = fmt.Fprintf(out, "  %s %s\n", command.Sprintf("%-12s", sub.Name()), sub.Short)
				break
			}
		}
	}
	type flagHelp struct{ name, usage string }
	for _, group := range []struct {
		name  string
		flags []flagHelp
	}{
		{"Rule selection", []flagHelp{
			{"strict", "Enable all stable rules, respecting configured exceptions"},
			{"experimental", "Enable nursery rules, respecting configured exceptions"},
		}},
		{"Flags", []flagHelp{
			{"config", "Read configuration from `PATH`"},
			{"ignore-regex", "Ignore findings matching `RE` (repeatable)"},
			{"json", "Write JSON results"},
			{"output-format", "Output `FMT`: text, oneline, json, jsonl, sarif, github"},
			{"output-file", "Write results to `PATH`"},
			{"quiet", "Suppress progress and summaries"},
			{"help", "Show help"},
			{"version", "Show version"},
		}},
	} {
		flags := pflag.NewFlagSet(group.name, pflag.ContinueOnError)
		flags.SortFlags = false
		for _, item := range group.flags {
			display := *a.root.Flags().Lookup(item.name)
			display.Usage = item.usage
			flags.AddFlag(&display)
		}
		_, _ = fmt.Fprintf(out, "\n%s\n%s", heading.Sprint(group.name+":"), styleHelpFlags(flags.FlagUsagesWrapped(width), option))
	}
	_, _ = fmt.Fprintln(out, "\nRun actionlint <command> --help for command options.")
	_, _ = fmt.Fprintln(out, "Exit codes: 0 clean · 1 findings · 2 invalid arguments · 3 failure")
	_, _ = fmt.Fprintln(out, "Docs: "+terminalLink(links, "https://actionlint.kjanat.dev", "https://actionlint.kjanat.dev"))
}

// The optional value is already part of the display name, so omit its type label.
type helpOptionalValue struct{ pflag.Value }

func (helpOptionalValue) Type() string { return "" }

func (a *commandApp) legacyHelp(c *cobra.Command) {
	a.helpShown = true
	out, colored, restore := a.helpOutput(c)
	defer restore()
	heading := helpStyle(colored, color.Bold, color.FgCyan)
	option := helpStyle(colored, color.FgCyan)
	_, _ = fmt.Fprintln(out, helpStyle(colored, color.Bold).Sprintf("actionlint %s", actionlint.Version()))
	_, _ = fmt.Fprintln(out, styleHelpFlags(`
  -format TEMPLATE        --template TEMPLATE
  -oneline                --output-format=oneline
  -ignore REGEX           --ignore-regex REGEX (repeatable, matches diagnostic messages)
  -config-file PATH       --config PATH
  -init / -init-config    config init
  -completion SHELL       completion SHELL
  -version                version
  -verbose / -debug       --log-level=info / --log-level=debug
  -color / -no-color      boolean options on the root; no-color wins
  -shellcheck COMMAND     false or an empty value disables ShellCheck
  -ruff COMMAND           false or an empty value disables Ruff
  -pyflakes COMMAND       Select Pyflakes rules through Ruff unless Ruff is selected
  -stdin-filename PATH    unchanged

Options may appear before or after filenames. -- ends option parsing.
Existing files named check, config, rules, doctor, completion or version win
over commands. Use --command NAME to select a command despite such a file.`, option))
	writeHelpDestinations(out, heading, a.helpHyperlinks())
}

func writeHelpDestinations(out io.Writer, heading *color.Color, links bool) {
	info, _ := debug.ReadBuildInfo()
	ref := documentationRef(actionlint.Version(), info)
	documentation := projectURL + "/tree/" + ref + "/docs/usage.md"
	display := documentation
	if len(ref) == 40 && !strings.ContainsAny(ref, "/.-") {
		display = projectURL + "/tree/" + ref[:12] + "/docs/usage.md"
	}
	_, _ = fmt.Fprintf(out, "\n%s\n  %s\n\n%s\n  %s\n", heading.Sprint("Project:"), terminalLink(links, projectURL, projectURL), heading.Sprint("Documentation:"), terminalLink(links, display, documentation))
}

type commandFlagDescription struct {
	Name        string   `json:"name"`
	Shorthand   string   `json:"shorthand,omitempty"`
	Type        string   `json:"type"`
	Default     string   `json:"default"`
	Description string   `json:"description"`
	Group       string   `json:"group,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Repeatable  bool     `json:"repeatable"`
}

type commandDescription struct {
	Name          string                   `json:"name"`
	Usage         string                   `json:"usage"`
	Description   string                   `json:"description"`
	Flags         []commandFlagDescription `json:"flags"`
	LegacyAliases map[string]string        `json:"legacy_aliases,omitempty"`
	ExitCodes     map[int]string           `json:"exit_codes"`
	Commands      []commandDescription     `json:"commands,omitempty"`
}

func describeCommand(c *cobra.Command) commandDescription {
	d := commandDescription{
		Name:        c.Name(),
		Usage:       c.UseLine(),
		Description: c.Long,
		Flags:       []commandFlagDescription{},
		ExitCodes:   map[int]string{0: "no findings", 1: "findings", 2: "invalid arguments", 3: "could not complete"},
	}
	if d.Description == "" {
		d.Description = c.Short
	}
	if c == c.Root() {
		d.LegacyAliases = map[string]string{"-flag": "--flag", "completions": "completion", "format": "template", "ignore": "ignore-regex", "config-file": "config", "init": "config init", "init-config": "config init"}
	}
	c.InitDefaultHelpFlag()
	annotateFlag(c.Flags(), "help", "Information")
	flags := pflag.NewFlagSet("description", pflag.ContinueOnError)
	flags.AddFlagSet(c.Flags())
	flags.AddFlagSet(c.PersistentFlags())
	flags.AddFlagSet(c.InheritedFlags())
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		_, description := pflag.UnquoteUsage(f)
		_, repeated := f.Value.(pflag.SliceValue)
		info := commandFlagDescription{Name: f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: f.DefValue, Description: description, Repeatable: repeated, Choices: f.Annotations[commandChoicesAnnotation]}
		if groups := f.Annotations[commandGroupAnnotation]; len(groups) > 0 {
			info.Group = groups[0]
		}
		d.Flags = append(d.Flags, info)
	})
	for _, sub := range c.Commands() {
		if !sub.Hidden && sub.Name() != "help" {
			d.Commands = append(d.Commands, describeCommand(sub))
		}
	}
	return d
}
