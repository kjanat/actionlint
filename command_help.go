package actionlint

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

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
	return commandBuildInfo{name, getCommandVersion(), getInstalledFrom(), runtime.Version(), runtime.GOOS, runtime.GOARCH}
}

func (a *commandApp) writeVersion() error {
	b := commandBuild()
	if a.jsonOutput() {
		return writeCommandJSON(a.streams.Stdout, b)
	}
	_, err := fmt.Fprintf(a.streams.Stdout, "%s %s\n%s\nbuilt with %s compiler for %s/%s\n", b.Name, b.Version, b.InstalledFrom, b.GoVersion, b.OS, b.GOARCH)
	return err
}

func (a *commandApp) help(c *cobra.Command, _ []string) {
	if a.jsonOutput() {
		if err := writeCommandJSON(a.streams.Stdout, describeCommand(c)); err != nil {
			a.status = a.reportError(err)
		}
		return
	}
	out := a.streams.Stderr
	_, _ = fmt.Fprintf(out, "%s\n\n%s\n\nUsage:\n  %s\n\nExamples:\n%s\n", c.Short, c.Long, c.Use, c.Example)
	for _, group := range commandHelpGroups {
		fs := pflag.NewFlagSet(group, pflag.ContinueOnError)
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if groups := f.Annotations[commandGroupAnnotation]; len(groups) > 0 && groups[0] == group {
				fs.AddFlag(f)
			}
		})
		_, _ = fmt.Fprintf(out, "\n%s:\n%s", group, fs.FlagUsagesWrapped(88))
	}
	ref := "HEAD"
	if v := getCommandVersion(); releaseVersionPattern.MatchString(v) {
		ref = "v" + v
	}
	_, _ = fmt.Fprintf(out, "\nCompatibility:\n  Existing -flag spellings still work. Place flags before files.\n  Use -- before filenames beginning with a dash.\n\nExit status:\n  0  No findings   1  Findings   2  Invalid arguments   3  Could not complete\n\nDocumentation:\n  https://github.com/kjanat/actionlint/tree/%s/docs/usage.md\n", ref)
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
	LegacyAliases map[string]string        `json:"legacy_aliases"`
	ExitCodes     map[int]string           `json:"exit_codes"`
}

func describeCommand(c *cobra.Command) commandDescription {
	d := commandDescription{
		Name:          c.Name(),
		Usage:         c.Use,
		Description:   c.Long,
		LegacyAliases: map[string]string{"-flag": "--flag", "completions": "completion", "h": "help"},
		ExitCodes:     map[int]string{0: "no findings", 1: "findings", 2: "invalid arguments", 3: "could not complete"},
	}
	c.Flags().VisitAll(func(f *pflag.Flag) {
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
	return d
}
