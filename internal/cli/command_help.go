package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"actionlint.kjanat.dev"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var commandHelpGroups = []string{"Input", "Output", "External linters", "Information"}
var releaseVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func writeCommandJSON(out io.Writer, value any) error {
	return json.NewEncoder(out).Encode(value)
}

type commandBuildInfo struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	InstalledFrom string `json:"installed_from"`
	GoVersion     string `json:"go_version"`
	OS            string `json:"os"`
	GOARCH        string `json:"goarch"`
}

func commandBuild() commandBuildInfo {
	name := "actionlint"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path != "" {
		name = info.Main.Path
	}
	return commandBuildInfo{name, actionlint.Version(), actionlint.InstalledFrom(), runtime.Version(), runtime.GOOS, runtime.GOARCH}
}

func writeVersion(out io.Writer, asJSON, legacy bool) error {
	b := commandBuild()
	if asJSON {
		return writeCommandJSON(out, b)
	}
	if !legacy {
		_, err := fmt.Fprintf(out, "actionlint %s\nInstalled: %s\nBuild: %s, %s/%s\n", b.Version, b.InstalledFrom, b.GoVersion, b.OS, b.GOARCH)
		return err
	}
	_, err := fmt.Fprintf(out, "%s %s\n%s\nbuilt with %s compiler for %s/%s\n", b.Name, b.Version, b.InstalledFrom, b.GoVersion, b.OS, b.GOARCH)
	return err
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
	out := a.streams.Stderr
	_, _ = fmt.Fprintf(out, "%s\n\nUsage:\n  %s\n", c.Short, c.UseLine())
	if c.Long != "" {
		_, _ = fmt.Fprintf(out, "\n%s\n", c.Long)
	}
	if c.Example != "" {
		_, _ = fmt.Fprintf(out, "\nExamples:\n%s\n", c.Example)
	}
	if c.HasAvailableSubCommands() {
		_, _ = fmt.Fprintln(out, "\nCommands:")
		for _, sub := range c.Commands() {
			if !sub.Hidden && sub.Name() != "help" {
				_, _ = fmt.Fprintf(out, "  %-12s %s\n", sub.Name(), sub.Short)
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
			_, _ = fmt.Fprintf(out, "\n%s:\n%s", group, usage)
		}
	}
	ref := "HEAD"
	if v := actionlint.Version(); releaseVersionPattern.MatchString(v) {
		ref = "v" + v
	}
	_, _ = fmt.Fprintf(out, "\nCompatibility:\n  Root options precede filenames. check accepts options after filenames.\n  Existing -flag and --flag spellings remain supported; see --help-legacy.\n  Use -- to force filenames, including names that match commands.\n\nExit status:\n  0  No findings   1  Findings   2  Invalid arguments   3  Could not complete\n\nDocumentation:\n  https://github.com/kjanat/actionlint/tree/%s/docs/usage.md\n", ref)
}

func (a *commandApp) legacyHelp() {
	a.helpShown = true
	_, _ = fmt.Fprintln(a.streams.Stderr, `Legacy root interface (both -name and --name remain supported):

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
Legacy options are supported without deprecation warnings.`)
}

// Debug output can arrive in fragments and from concurrent file checks. Emit
// complete JSON log records without interleaving their contents.
type commandJSONLogWriter struct {
	mu      sync.Mutex
	pending string
	out     io.Writer
}

func (w *commandJSONLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		if err := writeCommandJSON(w.out, struct {
			Log string `json:"log"`
		}{line}); err != nil {
			return 0, err
		}
	}
	return len(p), nil
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
