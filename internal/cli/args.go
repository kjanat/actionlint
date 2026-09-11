package cli

import (
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Normalize only option tokens. Values and filenames may themselves look like
// flags, and the legacy parser stops at the first positional argument.
func normalizeCommandArgs(flags *pflag.FlagSet, args []string) []string {
	if len(args) > 1 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		ret := append([]string{args[0]}, normalizeCommandArgs(flags, args[1:len(args)-1])...)
		return append(ret, args[len(args)-1])
	}
	ret := slices.Clone(args)
	for i := 0; i < len(ret); i++ {
		a := ret[i]
		if a == "--" || a == "-" || !strings.HasPrefix(a, "-") {
			break
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
	jsonMode, output := false, ""
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
			jsonMode, _ = strconv.ParseBool(value)
		case "output", "output-format":
			output = value
		}
	}
	return jsonMode || output == "json" || output == "jsonl"
}
