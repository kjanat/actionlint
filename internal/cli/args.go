package cli

import (
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type argumentFlag struct {
	flag  *pflag.Flag
	value string
	index int
}

type commandArgument struct {
	tokens                 []string
	flags                  []argumentFlag
	positional, terminated bool
}

// All CLI paths use the registered pflag definitions to distinguish option
// values, optional values, shorthand groups and positional arguments.
type argumentScanner struct {
	args  []string
	index int
}

func (s *argumentScanner) next(flags *pflag.FlagSet) (commandArgument, bool) {
	if s.index >= len(s.args) {
		return commandArgument{}, false
	}
	raw := s.args[s.index]
	s.index++
	arg := commandArgument{tokens: []string{raw}}
	if raw == "--" {
		arg.terminated = true
		return arg, true
	}
	if raw == "-" || !strings.HasPrefix(raw, "-") {
		arg.positional = true
		return arg, true
	}
	name, value, equals := strings.Cut(strings.TrimPrefix(raw, "-"), "=")
	if strings.HasPrefix(raw, "--") {
		name = strings.TrimPrefix(name, "-")
	}
	if flag := flags.Lookup(name); flag != nil {
		arg.tokens = nil
		s.option(&arg, flag, value, equals)
		return arg, true
	}
	if strings.HasPrefix(raw, "--") {
		return arg, true
	}
	arg.tokens = nil
	for short := raw[1:]; short != ""; {
		flag := flags.ShorthandLookup(short[:1])
		if flag == nil {
			arg.tokens = append(arg.tokens, "-"+short)
			break
		}
		short = short[1:]
		value, equals = "", false
		if strings.HasPrefix(short, "=") {
			value, equals, short = short[1:], true, ""
		} else if flag.NoOptDefVal == "" && short != "" {
			value, equals, short = short, true, ""
		}
		s.option(&arg, flag, value, equals)
	}
	return arg, true
}

func (s *argumentScanner) option(arg *commandArgument, flag *pflag.Flag, value string, equals bool) {
	index := len(arg.tokens)
	token := "--" + flag.Name
	if equals {
		token += "=" + value
		arg.tokens = append(arg.tokens, token)
	} else {
		arg.tokens = append(arg.tokens, token)
		value = flag.NoOptDefVal
		if value == "" && s.index < len(s.args) {
			value = s.args[s.index]
			s.index++
			arg.tokens = append(arg.tokens, value)
		}
	}
	arg.flags = append(arg.flags, argumentFlag{flag, value, index})
}

func commandFlags(command *cobra.Command) *pflag.FlagSet {
	command.InitDefaultHelpFlag()
	flags := pflag.NewFlagSet(command.Name(), pflag.ContinueOnError)
	flags.AddFlagSet(command.Flags())
	flags.AddFlagSet(command.PersistentFlags())
	flags.AddFlagSet(command.InheritedFlags())
	return flags
}

// Completion leaves the word being completed untouched; all preceding words use
// the same scanner and command definitions as normal invocations.
func normalizeCommandArgs(root *cobra.Command, args []string) []string {
	if len(args) > 1 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		ret := append([]string{args[0]}, normalizeCommandArgs(root, args[1:len(args)-1])...)
		return append(ret, args[len(args)-1])
	}
	command := root
	flags := commandFlags(command)
	scanner := argumentScanner{args: args}
	ret := make([]string, 0, len(args))
	positional := false
	for {
		arg, ok := scanner.next(flags)
		if !ok {
			break
		}
		ret = append(ret, arg.tokens...)
		if arg.terminated {
			ret = append(ret, args[scanner.index:]...)
			break
		}
		if arg.positional {
			found := false
			if !positional {
				for _, child := range command.Commands() {
					if arg.tokens[0] == child.Name() || slices.Contains(child.Aliases, arg.tokens[0]) {
						command, found = child, true
						flags = commandFlags(command)
						break
					}
				}
			}
			positional = positional || !found
		}
	}
	return ret
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
	scanner := argumentScanner{args: args}
	for {
		arg, ok := scanner.next(flags)
		if !ok || arg.terminated || arg.positional && !interspersed {
			break
		}
		for _, option := range arg.flags {
			switch option.flag.Name {
			case "json":
				formatSelected = true
				jsonMode, _ = strconv.ParseBool(option.value)
			case "json-pretty":
				options.PrettySet = true
				options.Pretty, _ = strconv.ParseBool(option.value)
			case "output", "output-format":
				formatSelected = true
				output = option.value
			case "template", "format", "template-file", "oneline":
				formatSelected = true
			}
		}
	}
	options.JSON = jsonMode || output == "json" || output == "jsonl" || options.Pretty && !formatSelected
	return options
}
