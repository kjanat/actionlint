package process

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// argInputs are the values argument placeholders take for one check.
// Variables come from preparation and the configuration transform; a list
// variable holds its items separated by newlines.
type argInputs struct {
	vars       map[string]string
	user       []string
	tomlPairs  []string
	configFile string
}

// renderArgs substitutes the closed placeholder grammar into argument
// templates. Placeholders are whole arguments and are never evaluated: a
// value always becomes exactly the arguments its placeholder names.
func renderArgs(templates []string, in argInputs) ([]string, error) {
	var args []string
	for _, template := range templates {
		inner, placeholder := strings.CutPrefix(template, "{")
		inner, closed := strings.CutSuffix(inner, "}")
		if !placeholder || !closed {
			args = append(args, template)
			continue
		}
		kind, rest, _ := strings.Cut(inner, ":")
		switch kind {
		case "var":
			if value := in.vars[rest]; value != "" {
				args = append(args, value)
			}
		case "flag":
			name, option, _ := strings.Cut(rest, ":")
			if value := in.vars[name]; value != "" {
				args = append(args, option, value)
			}
		case "switch":
			parts := strings.SplitN(rest, ":", 3)
			if len(parts) != 3 {
				return nil, fmt.Errorf("malformed argument placeholder %q", template)
			}
			chosen := parts[2]
			if truthy(in.vars[parts[0]]) {
				chosen = parts[1]
			}
			if chosen != "" {
				args = append(args, chosen)
			}
		case "list":
			name, separator, _ := strings.Cut(rest, ":")
			if value := in.vars[name]; value != "" {
				args = append(args, strings.Join(strings.Split(value, "\n"), separator))
			}
		case "config":
			switch {
			case strings.HasPrefix(rest, "toml-pairs:"):
				args = append(args, in.tomlPairs...)
			case rest == "toml-file", rest == "json-file":
				args = append(args, in.configFile)
			default:
				return nil, fmt.Errorf("unknown configuration placeholder %q", template)
			}
		case "user-args":
			args = append(args, in.user...)
		default:
			return nil, fmt.Errorf("unknown argument placeholder %q", template)
		}
	}
	return args, nil
}

// truthy reports whether a variable selects a switch's "on" argument.
func truthy(value string) bool {
	return value != "" && value != "false"
}

// launcher is a compiled provider.Launcher.
type launcher struct {
	spec                    provider.Launcher
	command, selector, tool *regexp.Regexp
}

func compileLaunchers(specs []provider.Launcher) ([]launcher, error) {
	compiled := make([]launcher, len(specs))
	for i, spec := range specs {
		compiled[i].spec = spec
		for _, field := range []struct {
			expression string
			target     **regexp.Regexp
		}{{spec.Command, &compiled[i].command}, {spec.Selector, &compiled[i].selector}, {spec.Tool, &compiled[i].tool}} {
			if field.expression == "" {
				continue
			}
			expression, err := regexp.Compile(`^(?:` + field.expression + `)$`)
			if err != nil {
				return nil, fmt.Errorf("launcher %d: %w", i, err)
			}
			*field.target = expression
		}
		if compiled[i].command == nil {
			return nil, fmt.Errorf("launcher %d has no command", i)
		}
	}
	return compiled, nil
}

// commandBase is an executable's lowercase base name without ".exe".
func commandBase(command string) string {
	return strings.TrimSuffix(strings.ToLower(command[strings.LastIndexAny(command, `/\`)+1:]), ".exe")
}

// splitLauncher separates the arguments of a launcher executable, which run
// the tool, from the arguments the tool itself receives. Executables that are
// no launcher pass every argument to the tool.
func splitLauncher(launchers []launcher, executable string, args []string) (prefix, rest []string, err error) {
	base := commandBase(executable)
	for _, l := range launchers {
		if !l.command.MatchString(base) {
			continue
		}
		prefix, rest, ok := l.split(args)
		if !ok {
			return nil, nil, fmt.Errorf("unsupported launcher arguments for %q: %q; %s", base, args, l.spec.Hint)
		}
		return prefix, rest, nil
	}
	return nil, args, nil
}

// split walks launcher options until an operand or module option names the
// tool. It fails on anything else.
func (l launcher) split(args []string) ([]string, []string, bool) {
	selected := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if l.spec.Separator && arg == "--" {
			if i+1 < len(args) && l.isTool(args[i+1]) {
				return args[:i+2], args[i+2:], true
			}
			return nil, nil, false
		}
		if !strings.HasPrefix(arg, "-") {
			if name, _, assignment := strings.Cut(arg, "="); l.spec.Assignments && assignment && name != "" {
				if slices.ContainsFunc(l.spec.Protected, func(protected string) bool { return strings.EqualFold(protected, name) }) {
					return nil, nil, false
				}
				continue
			}
			if l.isTool(arg) {
				return args[:i+1], args[i+1:], true
			}
			return nil, nil, false
		}
		if l.selector != nil && !selected && l.selector.MatchString(arg) {
			selected = true
			continue
		}
		if slices.Contains(l.spec.Flags, arg) {
			continue
		}
		option, value, attached := strings.Cut(arg, "=")
		if slices.Contains(l.spec.ValueOptions, option) {
			if !attached {
				i++
				if i == len(args) {
					return nil, nil, false
				}
				value = args[i]
			}
			if choices, restricted := l.spec.Choices[option]; restricted && !slices.Contains(choices, value) {
				return nil, nil, false
			}
			continue
		}
		if len(arg) < 2 || arg[1] == '-' {
			return nil, nil, false
		}
		next, done, ok := l.cluster(args, i)
		if !ok {
			return nil, nil, false
		}
		if done {
			return args[:next+1], args[next+1:], true
		}
		i = next
	}
	return nil, nil, false
}

// cluster reads the short options of args[i]. It returns the index of the
// last argument consumed and whether the module option ended the launcher.
func (l launcher) cluster(args []string, i int) (int, bool, bool) {
	letters := args[i][1:]
	for j := range len(letters) {
		letter := letters[j]
		switch {
		case l.spec.Module != nil && string(letter) == l.spec.Module.Short:
			rest := letters[j+1:]
			if rest == l.spec.Module.Name {
				return i, true, true
			}
			if rest == "" && i+1 < len(args) && args[i+1] == l.spec.Module.Name {
				return i + 1, true, true
			}
			return i, false, false
		case strings.IndexByte(l.spec.ShortFlags, letter) >= 0:
		case strings.IndexByte(l.spec.ShortValues, letter) >= 0:
			// The value is the rest of the cluster or the next, nonempty argument.
			if j+1 == len(letters) {
				i++
				if i == len(args) || args[i] == "" {
					return i, false, false
				}
			}
			return i, false, true
		default:
			return i, false, false
		}
	}
	return i, false, true
}

func (l launcher) isTool(arg string) bool {
	return l.tool != nil && !strings.HasPrefix(arg, "-") && l.tool.MatchString(arg)
}

// checkUserArgs applies a manifest's user-argument rules. Directory resolves
// relative values of inline options.
func checkUserArgs(name string, spec *provider.UserArgs, args []string, directory string) error {
	if spec == nil {
		return nil
	}
	for _, arg := range args {
		for _, rule := range spec.Forbidden {
			if rule.Prefix != "" && strings.HasPrefix(arg, rule.Prefix) {
				return fmt.Errorf("tools.%s: %s: %q", name, rule.Reason, arg)
			}
		}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" || arg == "--" {
			if !spec.Operands {
				return fmt.Errorf("tools.%s: extra file operands are not supported: %q", name, arg)
			}
			if arg == "--" {
				return nil
			}
			continue
		}
		option, value, attached := strings.Cut(arg, "=")
		short := !strings.HasPrefix(option, "--")
		for _, rule := range spec.Forbidden {
			if rule.Flag != "" && option == rule.Flag || rule.ShortLetter != "" && short && strings.Contains(option[1:], rule.ShortLetter) {
				return fmt.Errorf("tools.%s: %s: %q", name, rule.Reason, arg)
			}
		}
		if slices.Contains(spec.InlineOptions, option) {
			if !attached && i+1 < len(args) {
				value = args[i+1]
			}
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("tools.%s: option %q requires a value", name, option)
			}
			if !strings.HasPrefix(value, "-") && !inlineSetting(value, directory) {
				return fmt.Errorf("tools.%s: option %q takes inline KEY = VALUE settings, not a configuration file: %q", name, option, value)
			}
		}
		if slices.Contains(spec.ValueOptions, option) && !attached {
			if i+1 == len(args) || strings.HasPrefix(args[i+1], "-") {
				return fmt.Errorf("tools.%s: option %q requires a value", name, option)
			}
			i++
		}
	}
	return nil
}

// inlineSetting reports whether value is an inline KEY = VALUE setting: it
// names no existing file and has an unquoted "=" before any comment or line
// break.
func inlineSetting(value, directory string) bool {
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}
	if _, err := os.Stat(path); err == nil {
		return false
	}
	var quote rune
	escaped := false
	for _, char := range value {
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case char == '\\' && quote == '"':
				escaped = true
			case char == quote:
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '=':
			return true
		case '#', '\n', '\r':
			return false
		}
	}
	return false
}

// overriddenVars lists the preparation variables user options replace. Args
// are scanned as the tool reads them, skipping option values, until "--".
func overriddenVars(spec *provider.UserArgs, args []string) []string {
	if spec == nil || len(spec.Overrides) == 0 {
		return nil
	}
	var dropped []string
	drop := func(match func(provider.ArgOverride) bool) {
		for _, override := range spec.Overrides {
			if match(override) && !slices.Contains(dropped, override.Drops) {
				dropped = append(dropped, override.Drops)
			}
		}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if long, ok := strings.CutPrefix(arg, "--"); ok {
			option, _, attached := strings.Cut(long, "=")
			option = "--" + option
			if slices.Contains(spec.ValueOptions, option) && !attached && i+1 < len(args) {
				i++
			}
			drop(func(o provider.ArgOverride) bool { return o.Flag == option })
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		letters := arg[1:]
		for j := range len(letters) {
			letter := letters[j]
			drop(func(o provider.ArgOverride) bool { return o.Short == string(letter) })
			if strings.IndexByte(spec.AttachedShorts, letter) >= 0 {
				break
			}
			if strings.IndexByte(spec.ValueShorts, letter) >= 0 {
				if j+1 == len(letters) && i+1 < len(args) {
					i++
				}
				break
			}
		}
	}
	return dropped
}

// environmentOptions splits the values of variables a tool reads as extra
// options on spaces, without quoting, as the tool does. The command's own
// environment overrides the host's; host values count only when the manifest
// passes the variable through.
func environmentOptions(spec *provider.UserArgs, passthrough, env []string) ([]string, map[string]string) {
	if spec == nil || len(spec.EnvOptions) == 0 {
		return nil, nil
	}
	var args []string
	values := map[string]string{}
	for _, name := range spec.EnvOptions {
		value, ok := lookupEntry(env, name)
		if !ok && slices.ContainsFunc(passthrough, func(entry string) bool { return sameVariable(entry, name) }) {
			value, ok = os.LookupEnv(name)
		}
		if !ok || value == "" {
			continue
		}
		values[name] = value
		for field := range strings.SplitSeq(value, " ") {
			if field != "" {
				args = append(args, field)
			}
		}
	}
	return args, values
}

// lookupEntry finds the last NAME=VALUE entry for name.
func lookupEntry(env []string, name string) (string, bool) {
	var value string
	found := false
	for _, entry := range env {
		key, entryValue, _ := strings.Cut(entry, "=")
		if sameVariable(key, name) {
			value, found = entryValue, true
		}
	}
	return value, found
}

// sameVariable compares environment variable names as the host does.
func sameVariable(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// errNoUserArgs reports user arguments a manifest has no place for.
var errNoUserArgs = errors.New("this tool takes no extra arguments; its manifest has no {user-args} placeholder")
