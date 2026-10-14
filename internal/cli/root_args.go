package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/pflag"
)

// Root invocations retain Go's flag grammar. The command tree owns every flag
// definition; this adapter only preserves single-dash names, boolean values,
// early help and stopping at the first filename.
func (a *commandApp) parseRoot(args []string) (rest []string, forced string, terminated bool, err error) {
	f := flag.NewFlagSet("actionlint", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.Usage = func() {}
	a.root.Flags().VisitAll(func(v *pflag.Flag) {
		// Leave help to flag's early-exit handling, including -help=false.
		if v.Name == "help" {
			return
		}
		value := rootFlagValue{Value: v.Value}
		f.Var(value, v.Name, "")
		if v.Shorthand != "" {
			f.Var(value, v.Shorthand, "")
		}
	})
	jsonOptions := scanJSONOptions(a.root.Flags(), args, false)
	a.errorJSON = jsonOptions.JSON
	// --command selects the modern parser for everything after its value.
	// Option values that happen to contain --command or -- remain data.
	end := len(args)
	parsedArgs := slices.Clone(args)
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
			option := a.root.Flags().Lookup(name)
			if strings.HasPrefix(arg, "--") && option != nil && option.Value.Type() != "bool" && option.NoOptDefVal != "" {
				parsedArgs[i] += "=" + option.NoOptDefVal
				continue
			}
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
	err = f.Parse(parsedArgs[:end])
	f.Visit(func(v *flag.Flag) { a.set[v.Name] = true })
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			a.inv.JSON = a.errorJSON
			if jsonOptions.PrettySet {
				a.set["json-pretty"] = true
				a.inv.Render.PrettyJSON = jsonOptions.Pretty
			}
			a.help(a.root, nil)
			return nil, "", false, nil
		}
		return nil, "", false, commandUsageError{err}
	}
	forced = a.opts.command
	if a.set["command"] {
		if forced == "" {
			return nil, "", false, commandUsageError{errors.New("--command requires a command name")}
		}
		return args[end:], forced, false, nil
	}
	return f.Args(), forced, terminated, nil
}

// flag and pflag share the value contract, but flag also needs this marker to
// distinguish a bare boolean from an option that consumes the next argument.
type rootFlagValue struct{ pflag.Value }

func (v rootFlagValue) IsBoolFlag() bool { return v.Type() == "bool" }

func commandFileExists(name string) bool {
	if name == "pre-commit" {
		return false
	}
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
	return []string{"check", "pre-commit", "config", "rules", "doctor", "completion", "completions", "version", "help", "init", "init-config"}
}
