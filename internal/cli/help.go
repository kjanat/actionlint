package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"actionlint.kjanat.dev"
	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var commandHelpGroups = []string{"Input", "Output", "External linters", "Information"}

var releaseVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

var helpFlagPattern = regexp.MustCompile(`(?m)(^|[ \t])--?[a-zA-Z][a-zA-Z0-9-]*`)

func (a *commandApp) helpOutput(c *cobra.Command) (io.Writer, bool) {
	out := a.streams.Stderr
	file, ok := out.(*os.File)
	terminal := ok && (isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd()))
	enabled := a.helpColor(c, terminal)
	if terminal && enabled {
		out = colorable.NewColorable(file)
	}
	return out, enabled
}

func (a *commandApp) helpColor(c *cobra.Command, terminal bool) bool {
	if a.opts.noColor || a.jsonOutput() {
		return false
	}
	force := a.opts.color
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
	return force || (terminal && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb")
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
	c.InitDefaultHelpFlag()
	annotateFlag(c.Flags(), "help", "Information")
	if a.jsonOutput() {
		if err := writeCommandJSON(a.streams.Stdout, describeCommand(c)); err != nil {
			a.status = a.reportError(err)
		}
		return
	}
	out, colored := a.helpOutput(c)
	heading := helpStyle(colored, color.Bold, color.FgCyan)
	command := helpStyle(colored, color.Bold)
	option := helpStyle(colored, color.FgCyan)
	_, _ = fmt.Fprintf(out, "%s\n\n%s\n  %s\n", command.Sprint(c.Short), heading.Sprint("Usage:"), command.Sprint(c.UseLine()))
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
			if groups := f.Annotations[commandGroupAnnotation]; len(groups) > 0 && groups[0] == group {
				fs.AddFlag(f)
			}
		})
		if usage := fs.FlagUsagesWrapped(88); usage != "" {
			_, _ = fmt.Fprintf(out, "\n%s\n%s", heading.Sprint(group+":"), styleHelpFlags(usage, option))
		}
	}
	ref := "HEAD"
	if v := actionlint.Version(); releaseVersionPattern.MatchString(v) {
		ref = "v" + v
	}
	_, _ = fmt.Fprintf(out, "\n%s\n  Root options precede filenames. check accepts options after filenames.\n  Existing -flag and --flag spellings remain supported; see --help-legacy.\n  Use -- to force filenames, including names that match commands.\n\n%s\n  0  No findings   1  Findings   2  Invalid arguments   3  Could not complete\n\n%s\n  https://github.com/kjanat/actionlint/tree/%s/docs/usage.md\n", heading.Sprint("Compatibility:"), heading.Sprint("Exit status:"), heading.Sprint("Documentation:"), ref)
}

func (a *commandApp) legacyHelp(c *cobra.Command) {
	a.helpShown = true
	out, colored := a.helpOutput(c)
	heading := helpStyle(colored, color.Bold, color.FgCyan)
	option := helpStyle(colored, color.FgCyan)
	_, _ = fmt.Fprintln(out, heading.Sprint("Legacy root interface (both -name and --name remain supported):"))
	_, _ = fmt.Fprintln(out, styleHelpFlags(`
  -format TEMPLATE        --template TEMPLATE
  -oneline                --output-format=oneline
  -ignore REGEX           --ignore-regex REGEX (repeatable; message matching)
  -config-file PATH       --config PATH
  -init-config            config init
  -completion SHELL       completion SHELL (-completions also works)
  -version                version (the old flag keeps its original output)
  -verbose / -debug       --log-level=info / --log-level=debug
  -color / -no-color      boolean options on the root; no-color wins
  -shellcheck COMMAND     unchanged; an empty value disables ShellCheck
  -pyflakes COMMAND       unchanged; an empty value disables Pyflakes
  -stdin-filename PATH    unchanged

Root parsing stops at the first filename. -- ends option parsing.
Existing files named check, config, rules, doctor, completion or version win
over commands. Use --command NAME to select a command despite such a file.
Legacy options are supported without deprecation warnings.`, option))
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
		d.LegacyAliases = map[string]string{"-flag": "--flag", "completions": "completion", "format": "template", "ignore": "ignore-regex", "config-file": "config"}
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
