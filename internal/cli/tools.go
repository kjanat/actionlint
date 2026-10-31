package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/pflag"

	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

// deprecatedToolFlag is one use of a deprecated flag whose value is ignored,
// as declared in a bundled provider manifest.
type deprecatedToolFlag struct {
	id    string
	flag  provider.DeprecatedFlag
	value string
}

// externalToolFlags registers --tool, --no-tool and --allow-config-processes.
// It also registers, hidden, each flag listed in the deprecated_flags field
// of a bundled provider manifest. Using one of those records a deprecation
// warning.
func (a *commandApp) externalToolFlags(f *pflag.FlagSet) {
	c := &a.inv.Check
	f.Var(toolListValue{&c.ToolFlags}, "tool", "Run the tool `ID[=COMMAND]` with its default command or COMMAND; ID=false disables it (repeatable)")
	f.Var(noToolValue{&c.ToolFlags}, "no-tool", "Disable the tool `ID`, like --tool ID=false (repeatable)")
	f.BoolVar(&c.AllowConfigProcesses, "allow-config-processes", false, "Allow trusted repository configuration to select native executables")
	for _, name := range []string{"tool", "no-tool", "allow-config-processes"} {
		annotateFlag(f, name, "External linters")
	}
	manifests, err := provider.Bundled()
	if err != nil {
		// The bundled manifests are a build input that tests validate.
		panic(err)
	}
	for _, manifest := range manifests {
		id := manifest.Rule.Name
		for _, retired := range manifest.DeprecatedFlags {
			if f.Lookup(retired.Name) != nil {
				panic(fmt.Sprintf("bundled tool %s: flag --%s already exists", id, retired.Name))
			}
			f.AddFlag(&pflag.Flag{
				Name:        retired.Name,
				Value:       deprecatedFlagValue{id, retired, c},
				Usage:       "Deprecated: use --tool " + id + " or --no-tool " + id,
				Hidden:      true,
				Annotations: map[string][]string{commandGroupAnnotation: {"External linters"}},
			})
		}
	}
}

// deprecatedFlagHelp returns one --help-legacy line for each retired flag
// listed in the deprecated_flags field of a bundled provider manifest.
func deprecatedFlagHelp() string {
	manifests, err := provider.Bundled()
	if err != nil {
		// externalToolFlags already read the same manifests.
		panic(err)
	}
	var lines strings.Builder
	for _, manifest := range manifests {
		id := manifest.Rule.Name
		for _, retired := range manifest.DeprecatedFlags {
			name, usage := "-"+retired.Name+" COMMAND", "--tool "+id+"=COMMAND"
			if retired.Value == provider.DeprecatedFlagIgnored {
				name, usage = "-"+retired.Name+" VALUE", "runs "+id
				if len(retired.Arguments) != 0 {
					usage += " adding " + strings.Join(retired.Arguments, " ")
				}
			}
			fmt.Fprintf(&lines, "  %-22s  %s; empty or false disables %s (deprecated)\n", name, usage, id)
		}
	}
	return lines.String()
}

// toolListValue appends every value to a shared list, so --tool, --no-tool
// and the deprecated command flags keep their command-line order.
type toolListValue struct{ target *[]string }

func (v toolListValue) String() string { return strings.Join(*v.target, ",") }

func (v toolListValue) Set(value string) error {
	*v.target = append(*v.target, value)
	return nil
}

func (toolListValue) Type() string { return "stringArray" }

// noToolValue is --no-tool ID. It appends ID=false to the --tool values.
type noToolValue struct{ target *[]string }

func (v noToolValue) String() string { return "" }

func (v noToolValue) Set(id string) error {
	if id == "" || strings.Contains(id, "=") {
		return fmt.Errorf("%q is not a tool id", id)
	}
	*v.target = append(*v.target, id+"=false")
	return nil
}

func (noToolValue) Type() string { return "stringArray" }

// deprecatedFlagValue is a flag listed in the deprecated_flags field of a
// bundled provider manifest. A command flag appends ID=VALUE to the --tool
// values; an ignored-value flag is recorded for defaultSelection.
type deprecatedFlagValue struct {
	id    string
	flag  provider.DeprecatedFlag
	check *checkInvocation
}

func (v deprecatedFlagValue) String() string { return "" }

func (v deprecatedFlagValue) Set(value string) error {
	switch v.flag.Value {
	case provider.DeprecatedFlagCommand:
		v.check.ToolFlags = append(v.check.ToolFlags, v.id+"="+value)
		v.check.Deprecations = append(v.check.Deprecations, fmt.Sprintf("--%s is deprecated; use --tool %s=COMMAND or --no-tool %s", v.flag.Name, v.id, v.id))
	case provider.DeprecatedFlagIgnored:
		v.check.DeprecatedToolFlags = append(v.check.DeprecatedToolFlags, deprecatedToolFlag{v.id, v.flag, value})
		v.check.Deprecations = append(v.check.Deprecations, fmt.Sprintf("--%s is deprecated; use --tool %s or --no-tool %s", v.flag.Name, v.id, v.id))
	default:
		return fmt.Errorf("--%s: manifest of %s declares unknown value kind %q", v.flag.Name, v.id, v.flag.Value)
	}
	return nil
}

func (deprecatedFlagValue) Type() string { return "string" }

// disablesTool reports whether a --tool command or deprecated flag value
// turns the tool off: an empty value or false.
func disablesTool(value string) bool {
	return value == "" || strings.EqualFold(value, "false")
}

// resolveTools selects every bundled tool from --tool, --no-tool, the
// deprecated flags bundled manifests declare and the ACTIONLINT_TOOL_<ID>_*
// environment variables. The last --tool, --no-tool or deprecated command
// flag given for a tool decides; without one, the environment variables do;
// without those, the tool runs its default command, which is skipped with a
// warning when it cannot run. A deprecated flag whose value is ignored
// changes only that default.
func (c checkInvocation) resolveTools() (resolvedTools, error) {
	manifests, err := provider.Bundled()
	if err != nil {
		return resolvedTools{}, err
	}
	bundled := func(id string) bool {
		return slices.ContainsFunc(manifests, func(m provider.Manifest) bool { return m.Rule.Name == id })
	}
	var resolved resolvedTools
	commands := map[string]string{}
	for _, value := range c.ToolFlags {
		id, command, explicit := strings.Cut(value, "=")
		if id == "" {
			return resolvedTools{}, commandUsageError{fmt.Errorf("--tool %q names no tool", value)}
		}
		if !explicit {
			command = id
		}
		if !bundled(id) && !disablesTool(command) && command != id {
			return resolvedTools{}, commandUsageError{fmt.Errorf("--tool %s: only bundled tools take a command", value)}
		}
		if _, seen := commands[id]; !seen {
			resolved.named = append(resolved.named, id)
		}
		commands[id] = command
	}
	for _, id := range resolved.named {
		if disablesTool(commands[id]) {
			resolved.disabled = append(resolved.disabled, id)
		}
	}
	resolved.tools = make(registry.Selections, len(manifests))
	for _, manifest := range manifests {
		id := manifest.Rule.Name
		command, explicit := commands[id]
		switch {
		case slices.Contains(resolved.disabled, id):
			resolved.tools[id] = backend.Selection{}
		case explicit:
			resolved.tools[id] = backend.Selection{Command: command, Process: &processconfig.Options{IgnoreConfig: true}}
		default:
			options, err := toolEnvironment(id)
			if err != nil {
				return resolvedTools{}, err
			}
			if options != nil {
				resolved.tools[id] = backend.Selection{Command: id, Process: options}
				continue
			}
			selection, disabled := defaultSelection(id, c.DeprecatedToolFlags)
			resolved.tools[id] = selection
			if disabled {
				resolved.disabled = append(resolved.disabled, id)
			}
		}
	}
	return resolved, nil
}

// resolvedTools is how resolveTools selects the external tools.
type resolvedTools struct {
	// tools selects every bundled tool.
	tools registry.Selections
	// disabled lists the tool ids the flags disabled.
	disabled []string
	// named lists the tool ids --tool and --no-tool named.
	named []string
}

// defaultSelection returns the selection of the bundled tool id when no flag
// or environment variable selected it: its default command, changed by the
// last deprecated flag with an ignored value used for id. It also reports
// whether that flag disabled the tool.
func defaultSelection(id string, used []deprecatedToolFlag) (backend.Selection, bool) {
	selection := backend.Selection{Command: id, Process: &processconfig.Options{Optional: true}}
	for _, flag := range slices.Backward(used) {
		if flag.id != id {
			continue
		}
		if disablesTool(flag.value) {
			return backend.Selection{}, true
		}
		selection.Process.Arguments = slices.Clone(flag.flag.Arguments)
		break
	}
	return selection, false
}

// toolEnvironment reads the ACTIONLINT_TOOL_<ID>_BIN, _FLAGS and _ENV
// variables of the tool id, where <ID> is id upper-cased with dashes
// replaced by underscores. It returns nil options when none of the
// variables is set.
func toolEnvironment(id string) (*processconfig.Options, error) {
	prefix := "ACTIONLINT_TOOL_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_"))
	return externalToolEnvironment(func(suffix string) (string, string, bool) {
		value, ok := os.LookupEnv(prefix + suffix)
		return prefix + suffix, value, ok
	})
}
