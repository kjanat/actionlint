package cli

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

// deprecatedToolFlag is one use of a retired flag that a bundled provider
// manifest declares.
type deprecatedToolFlag struct {
	id    string
	flag  provider.DeprecatedFlag
	value string
}

// externalToolFlags registers --tool, --no-tool and --allow-config-processes.
// For every bundled provider it also registers hidden deprecated aliases:
// --ID[=COMMAND] for --tool ID=COMMAND, --no-ID for --no-tool ID, and the
// retired flags the provider's manifest declares. Each alias records a
// deprecation warning when it is used.
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
		aliases := []*pflag.Flag{
			{Name: id, Value: toolAliasValue{id, c}, NoOptDefVal: id, Usage: "Deprecated: use --tool " + id},
			{Name: "no-" + id, Value: noToolAliasValue{id, c}, NoOptDefVal: "true", Usage: "Deprecated: use --no-tool " + id},
		}
		for _, retired := range manifest.DeprecatedFlags {
			aliases = append(aliases, &pflag.Flag{Name: retired.Name, Value: deprecatedFlagValue{id, retired, c}, Usage: "Deprecated: use --tool " + id + " or --no-tool " + id})
		}
		for _, alias := range aliases {
			if f.Lookup(alias.Name) != nil {
				panic(fmt.Sprintf("bundled tool %s: flag --%s already exists", id, alias.Name))
			}
			alias.Hidden = true
			alias.Annotations = map[string][]string{commandGroupAnnotation: {"External linters"}}
			f.AddFlag(alias)
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
			usage := "runs " + id
			if len(retired.Arguments) != 0 {
				usage += " adding " + strings.Join(retired.Arguments, " ")
			}
			fmt.Fprintf(&lines, "  %-22s  %s; empty or false disables %s (deprecated)\n", "-"+retired.Name+" VALUE", usage, id)
		}
	}
	return lines.String()
}

// toolListValue appends every value to a shared list, so --tool, --no-tool
// and their deprecated aliases keep their command-line order.
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

// toolAliasValue is the deprecated flag --ID[=COMMAND]. It appends
// ID=COMMAND to the --tool values.
type toolAliasValue struct {
	id    string
	check *checkInvocation
}

func (v toolAliasValue) String() string { return "" }

func (v toolAliasValue) Set(value string) error {
	v.check.ToolFlags = append(v.check.ToolFlags, v.id+"="+value)
	v.check.Deprecations = append(v.check.Deprecations, fmt.Sprintf("--%s is deprecated; use --tool %s=COMMAND or --no-tool %s", v.id, v.id, v.id))
	return nil
}

func (toolAliasValue) Type() string { return "string" }

// noToolAliasValue is the deprecated flag --no-ID. When true, it appends
// ID=false to the --tool values, as --no-tool ID does.
type noToolAliasValue struct {
	id    string
	check *checkInvocation
}

func (v noToolAliasValue) String() string { return "false" }

func (v noToolAliasValue) Set(value string) error {
	disable, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	if disable {
		v.check.ToolFlags = append(v.check.ToolFlags, v.id+"=false")
	}
	v.check.Deprecations = append(v.check.Deprecations, fmt.Sprintf("--no-%s is deprecated; use --no-tool %s", v.id, v.id))
	return nil
}

func (noToolAliasValue) Type() string { return "bool" }

// deprecatedFlagValue is a retired flag a bundled provider manifest declares.
type deprecatedFlagValue struct {
	id    string
	flag  provider.DeprecatedFlag
	check *checkInvocation
}

func (v deprecatedFlagValue) String() string { return "" }

func (v deprecatedFlagValue) Set(value string) error {
	v.check.DeprecatedToolFlags = append(v.check.DeprecatedToolFlags, deprecatedToolFlag{v.id, v.flag, value})
	v.check.Deprecations = append(v.check.Deprecations, fmt.Sprintf("--%s is deprecated; use --tool %s or --no-tool %s", v.flag.Name, v.id, v.id))
	return nil
}

func (deprecatedFlagValue) Type() string { return "string" }

// disablesTool reports whether a --tool command or deprecated flag value
// turns the tool off: an empty value or false.
func disablesTool(value string) bool {
	return value == "" || strings.EqualFold(value, "false")
}

// resolveTools selects every bundled tool from --tool, --no-tool, their
// deprecated aliases and the ACTIONLINT_TOOL_<ID>_* environment variables.
// The last --tool or --no-tool given for a tool decides; without one, the
// environment variables do; without those, the tool runs its default
// command, which is skipped with a warning when it cannot run. A retired
// flag changes only that default.
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
			options, deprecations, err := toolEnvironment(id)
			if err != nil {
				return resolvedTools{}, err
			}
			resolved.deprecations = append(resolved.deprecations, deprecations...)
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
	// deprecations holds a warning for each deprecated environment
	// variable read.
	deprecations []string
}

// defaultSelection returns the selection of the bundled tool id when no flag
// or environment variable selected it: its default command, changed by the
// last of the retired flags used that is declared for id. It also reports
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
// replaced by underscores. The former ACTIONLINT_<ID>_* variables are
// deprecated aliases; a set new variable wins over its alias. It returns
// nil options when none of the variables is set, and a warning for every
// alias it read.
func toolEnvironment(id string) (*processconfig.Options, []string, error) {
	key := strings.ToUpper(strings.ReplaceAll(id, "-", "_"))
	current, former := "ACTIONLINT_TOOL_"+key, "ACTIONLINT_"+key
	var deprecations []string
	lookup := func(suffix string) (string, string, bool) {
		if value, ok := os.LookupEnv(current + suffix); ok {
			return current + suffix, value, true
		}
		if value, ok := os.LookupEnv(former + suffix); ok {
			deprecations = append(deprecations, fmt.Sprintf("%s is deprecated; use %s", former+suffix, current+suffix))
			return former + suffix, value, true
		}
		return "", "", false
	}
	options, err := externalToolEnvironment(lookup)
	return options, deprecations, err
}
