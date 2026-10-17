package cli

import (
	"errors"
	"io"
	"os"
	"slices"
	"sync"

	"github.com/spf13/pflag"
)

// Split root options from command routing, then parse them with the same pflag
// definitions and argument scanner used for subcommands and completion.
func (a *commandApp) parseRoot(args []string) (rest []string, forced string, terminated bool, err error) {
	flags := a.root.Flags()
	flags.SetOutput(io.Discard)
	scanner := argumentScanner{args: args}
	parsedArgs := make([]string, 0, len(args))
	hasInput := false
	helpIndex := -1
	for {
		arg, ok := scanner.next(flags)
		if !ok {
			break
		}
		if arg.terminated {
			terminated = true
			parsedArgs = append(parsedArgs, arg.tokens...)
			parsedArgs = append(parsedArgs, args[scanner.index:]...)
			break
		}
		if arg.positional {
			name := arg.tokens[0]
			if !hasInput && isCommandName(name) && !commandFileExists(name) {
				rest = append([]string{name}, args[scanner.index:]...)
				break
			}
			hasInput = true
		}
		for _, option := range arg.flags {
			if option.flag.Name == "help" && helpIndex < 0 {
				helpIndex = len(parsedArgs) + option.index
			}
		}
		parsedArgs = append(parsedArgs, arg.tokens...)
		if slices.ContainsFunc(arg.flags, func(option argumentFlag) bool { return option.flag.Name == "command" }) {
			rest = slices.Clone(args[scanner.index:])
			break
		}
	}
	jsonOptions := scanJSONOptions(flags, parsedArgs, true)
	a.errorJSON = jsonOptions.JSON
	// Preserve root help's early exit, including the legacy -help=false spelling.
	if helpIndex >= 0 {
		parsedArgs = append(parsedArgs[:helpIndex], "--help")
	}
	err = flags.Parse(parsedArgs)
	flags.Visit(func(flag *pflag.Flag) { a.set[flag.Name] = true })
	if err != nil {
		return nil, "", false, commandUsageError{err}
	}
	if helpIndex >= 0 {
		a.inv.JSON = a.errorJSON
		if jsonOptions.PrettySet {
			a.set["json-pretty"] = true
			a.inv.Render.PrettyJSON = jsonOptions.Pretty
		}
		a.help(a.root, nil)
		return nil, "", false, nil
	}
	rest = append(slices.Clone(flags.Args()), rest...)
	forced = a.opts.command
	if a.set["command"] {
		if forced == "" {
			return nil, "", false, commandUsageError{errors.New("--command requires a command name")}
		}
		return rest, forced, false, nil
	}
	return rest, forced, terminated, nil
}

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
