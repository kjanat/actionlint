package cli

import (
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Accept the root's legacy single-dash names on subcommands too. Consume option
// values before looking for commands or aliases, and preserve literal filenames.
func normalizeSubcommandArgs(root *cobra.Command, args []string) []string {
	ret := slices.Clone(args)
	command := root
	positional := false
	for i := 0; i < len(ret); i++ {
		arg := ret[i]
		if arg == "--" {
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			found := false
			if !positional {
				for _, child := range command.Commands() {
					if arg == child.Name() || slices.Contains(child.Aliases, arg) {
						command, found = child, true
						break
					}
				}
			}
			positional = positional || !found
			continue
		}
		name, _, equals := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		flag := command.Flags().Lookup(name)
		if flag == nil {
			flag = command.PersistentFlags().Lookup(name)
		}
		if flag == nil {
			flag = command.InheritedFlags().Lookup(name)
		}
		if flag != nil && !strings.HasPrefix(arg, "--") && len(name) > 1 {
			ret[i] = "-" + arg
		}
		if flag == nil && !strings.HasPrefix(arg, "--") && len(name) == 1 {
			flag = command.Flags().ShorthandLookup(name)
			if flag == nil {
				flag = command.PersistentFlags().ShorthandLookup(name)
			}
			if flag == nil {
				flag = command.InheritedFlags().ShorthandLookup(name)
			}
		}
		if flag != nil && flag.NoOptDefVal == "" && !equals {
			i++
		}
	}
	return ret
}

// Normalize root option tokens, including those between filenames. Stop at a
// command or --, and leave option values untouched.
func normalizeCommandArgs(flags *pflag.FlagSet, args []string) []string {
	if len(args) > 1 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		ret := append([]string{args[0]}, normalizeCommandArgs(flags, args[1:len(args)-1])...)
		return append(ret, args[len(args)-1])
	}
	ret := slices.Clone(args)
	hasInput := false
	for i := 0; i < len(ret); i++ {
		a := ret[i]
		if a == "--" {
			break
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			if !hasInput && isCommandName(a) && !commandFileExists(a) {
				break
			}
			hasInput = true
			continue
		}
		name, value, equals := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-"), "=")
		if name == "h" || name == "help" {
			ret[i] = "--help"
			// Go's help flag exits immediately, including with an explicit value.
			// The new JSON help modifier is the only trailing option consumed here.
			if commandRequestsJSON(flags, ret[i+1:]) {
				return append(ret[:i+1], "--json")
			}
			return ret[:i+1]
		}
		f := flags.Lookup(name)
		if f != nil {
			ret[i] = "--" + f.Name
			if equals {
				ret[i] += "=" + value
			}
		} else if len(name) == 1 && !strings.HasPrefix(a, "--") {
			f = flags.ShorthandLookup(name)
		}
		if f != nil && f.NoOptDefVal == "" && !equals {
			i++
		}
	}
	return ret
}

// Select the error format even when a bad option precedes --json. Known option
// values and filenames remain data during this scan.
func commandRequestsJSON(flags *pflag.FlagSet, args []string) bool {
	return requestsJSON(flags, args, false)
}

func requestsJSON(flags *pflag.FlagSet, args []string, interspersed bool) bool {
	return scanJSONOptions(flags, args, interspersed).JSON
}

type requestedJSONOptions struct {
	JSON, Pretty, PrettySet bool
}

func scanJSONOptions(flags *pflag.FlagSet, args []string, interspersed bool) requestedJSONOptions {
	jsonMode, output := false, ""
	formatSelected := false
	options := requestedJSONOptions{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			if interspersed {
				continue
			}
			break
		}
		name, value, equal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		flag := flags.Lookup(name)
		if flag == nil && len(name) >= 1 && !strings.HasPrefix(arg, "--") {
			flag = flags.ShorthandLookup(name[:1])
			if flag != nil && flag.NoOptDefVal == "" && len(name) > 1 {
				if equal {
					value = name[1:] + "=" + value
				} else {
					value = name[1:]
				}
				equal = true
			}
		}
		if flag == nil {
			continue
		}
		if !equal {
			value = flag.NoOptDefVal
			if value == "" && i+1 < len(args) {
				i++
				value = args[i]
			}
		}
		switch flag.Name {
		case "json":
			formatSelected = true
			jsonMode, _ = strconv.ParseBool(value)
		case "json-pretty":
			options.PrettySet = true
			options.Pretty, _ = strconv.ParseBool(value)
		case "output", "output-format":
			formatSelected = true
			output = value
		case "template", "format", "template-file", "oneline":
			formatSelected = true
		}
	}
	options.JSON = jsonMode || output == "json" || output == "jsonl" || options.Pretty && !formatSelected
	return options
}
