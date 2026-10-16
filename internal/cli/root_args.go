package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/pflag"
)

// The command tree owns every flag definition. This adapter accepts options
// between filenames while preserving single-dash names, boolean values and early
// help. Commands and -- delimit the root options.
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
	// --command selects the modern parser for everything after its value.
	// Option values that happen to contain --command or -- remain data.
	parsedArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			terminated = true
			rest = append(rest, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			if len(rest) == 0 && isCommandName(arg) && !commandFileExists(arg) {
				rest = append(rest, args[i:]...)
				break
			}
			rest = append(rest, arg)
			continue
		}
		parsedArgs = append(parsedArgs, arg)
		name, _, equals := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		v := f.Lookup(name)
		if v == nil {
			continue
		}
		if !equals {
			option := a.root.Flags().Lookup(name)
			if strings.HasPrefix(arg, "--") && option != nil && option.Value.Type() != "bool" && option.NoOptDefVal != "" {
				parsedArgs[len(parsedArgs)-1] += "=" + option.NoOptDefVal
				continue
			}
			b, ok := v.Value.(interface{ IsBoolFlag() bool })
			if !ok || !b.IsBoolFlag() {
				if i+1 < len(args) {
					i++
					parsedArgs = append(parsedArgs, args[i])
				}
			}
		}
		if name == "command" {
			rest = append(rest, args[i+1:]...)
			break
		}
	}
	jsonOptions := scanJSONOptions(a.root.Flags(), parsedArgs, false)
	a.errorJSON = jsonOptions.JSON
	err = f.Parse(parsedArgs)
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
		return rest, forced, false, nil
	}
	return rest, forced, terminated, nil
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

var registeredCommandNames = sync.OnceValue(func() []string {
	app := newCommandApp(&Command{})
	names := []string{"help"}
	for _, command := range app.root.Commands() {
		names = append(names, command.Name())
		names = append(names, command.Aliases...)
	}
	return names
})

func commandNames() []string { return slices.Clone(registeredCommandNames()) }
