// Package commands models interpreter invocations without assigning a language
// to an executable that has no registered definition.
package commands

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
)

type Status string

type ArgumentPlatform string

const (
	UnixArguments    ArgumentPlatform = "unix"
	WindowsArguments ArgumentPlatform = "windows"
	UnknownArguments ArgumentPlatform = "unknown"
)

const (
	Valid   Status = "valid"
	Invalid Status = "invalid"
	Unknown Status = "unknown"
)

// Token offsets address the original command, before runner substitution.
type Token struct {
	Value string `json:"value"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type Issue struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
}

type Setting struct {
	Group   string `json:"group"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// Option describes one interpreter option. Choices validates a required next
// argument; Group records its named setting. Effect records a fixed setting.
// Input changes where the interpreter reads source (command or stdin).
type Option struct {
	Short     string   `json:"short,omitempty"`
	Long      string   `json:"long,omitempty"`
	Argument  bool     `json:"argument,omitempty"`
	Choices   []string `json:"choices,omitempty"`
	Group     string   `json:"group,omitempty"`
	Effect    string   `json:"effect,omitempty"`
	Input     string   `json:"input,omitempty"`
	Warning   string   `json:"warning,omitempty"`
	ExitAfter string   `json:"exit_after,omitempty"`
}

// Definition belongs to an interpreter or wrapper provider. Prefix selects a
// wrapper subcommand; Delegate validates the following arguments with another
// registered definition. Extends borrows its options and language. IDs can name
// definitions supplied by another plugin; unresolved IDs produce Unknown.
type Definition struct {
	ID                   string   `json:"id"`
	Executables          []string `json:"executables,omitempty"`
	IgnoreExecutableCase bool     `json:"ignore_executable_case,omitempty"`
	Prefix               []string `json:"prefix,omitempty"`
	Extends              string   `json:"extends,omitempty"`
	Delegate             string   `json:"delegate,omitempty"`
	Language             string   `json:"language,omitempty"`
	Options              []Option `json:"options,omitempty"`
	ToggleOptions        bool     `json:"toggle_options,omitempty"`
	LongOptionsFirst     bool     `json:"long_options_first,omitempty"`
	UnknownOptions       bool     `json:"unknown_options,omitempty"`
	DefaultArguments     []string `json:"default_arguments,omitempty"`
	ImplicitArguments    []string `json:"implicit_arguments,omitempty"`
}

type Invocation struct {
	Command      string    `json:"command"`
	Program      string    `json:"program"`
	Arguments    []Token   `json:"arguments"`
	DefinitionID string    `json:"definition_id,omitempty"`
	Status       Status    `json:"status"`
	Reason       string    `json:"reason,omitempty"`
	Language     string    `json:"language,omitempty"`
	Input        string    `json:"input"`
	Settings     []Setting `json:"settings,omitempty"`
	Issues       []Issue   `json:"issues,omitempty"`
}

// Registry owns copies of provider definitions. Analysis never modifies them.
type Registry struct{ definitions []Definition }

type registryKey struct{}

func WithRegistry(ctx context.Context, registry *Registry) context.Context {
	return context.WithValue(ctx, registryKey{}, registry)
}

// FromContext returns nil when a lower-level caller supplies no session context.
func FromContext(ctx context.Context) *Registry {
	if ctx == nil {
		return nil
	}
	registry, _ := ctx.Value(registryKey{}).(*Registry)
	return registry
}

func New(definitions []Definition) (*Registry, error) {
	r := &Registry{}
	ids := make(map[string]bool)
	for _, definition := range definitions {
		if definition.ID == "" || ids[definition.ID] {
			return nil, fmt.Errorf("duplicate or empty interpreter definition %q", definition.ID)
		}
		ids[definition.ID] = true
		for _, option := range definition.Options {
			switch {
			case option.Input != "" && option.Input != "command" && option.Input != "stdin":
				return nil, fmt.Errorf("invalid source mode %q in interpreter definition %q", option.Input, definition.ID)
			case option.ExitAfter != "" && option.ExitAfter != "option" && option.ExitAfter != "long-options":
				return nil, fmt.Errorf("invalid exit phase %q in interpreter definition %q", option.ExitAfter, definition.ID)
			case len(option.Choices) != 0 && !option.Argument:
				return nil, fmt.Errorf("option choices require an argument in interpreter definition %q", definition.ID)
			case len(option.Short) > 1 || option.Short == "" && option.Long == "":
				return nil, fmt.Errorf("invalid option name in interpreter definition %q", definition.ID)
			case option.Group != "" && !option.Argument && option.Effect == "":
				return nil, fmt.Errorf("setting option requires an argument or fixed effect in interpreter definition %q", definition.ID)
			}
		}
		definition = CloneDefinitions([]Definition{definition})[0]
		r.definitions = append(r.definitions, definition)
	}
	return r, nil
}

func ExecutableName(program string) string {
	return path.Base(strings.ReplaceAll(program, `\`, "/"))
}

// Arguments follows the .NET process argument rules used by the runner, not
// POSIX shell parsing: single quotes are literal. Backslashes escape double
// quotes according to CRT rules. No expansion or command execution occurs.
// https://github.com/dotnet/runtime/blob/v9.0.0/src/libraries/System.Diagnostics.Process/src/System/Diagnostics/Process.Unix.cs
func Arguments(text string, offset int) []Token {
	var tokens []Token
	for i := 0; i < len(text); {
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			i++
		}
		if i == len(text) {
			break
		}
		start, quoted := i, false
		var value strings.Builder
		for i < len(text) {
			if text[i] == '\\' {
				count := 0
				for i < len(text) && text[i] == '\\' {
					i++
					count++
				}
				if i < len(text) && text[i] == '"' {
					value.WriteString(strings.Repeat(`\`, count/2))
					if count%2 != 0 {
						value.WriteByte('"')
						i++
					}
				} else {
					value.WriteString(strings.Repeat(`\`, count))
				}
				continue
			}
			if text[i] == '"' {
				if quoted && i+1 < len(text) && text[i+1] == '"' {
					value.WriteByte('"')
					i++
				} else {
					quoted = !quoted
				}
				i++
				continue
			}
			if !quoted && (text[i] == ' ' || text[i] == '\t') {
				break
			}
			value.WriteByte(text[i])
			i++
		}
		tokens = append(tokens, Token{Value: value.String(), Start: offset + start, End: offset + i})
	}
	return tokens
}

func (r *Registry) Analyze(command string, implicit bool) Invocation {
	return r.AnalyzeFor(command, implicit, UnknownArguments)
}

func (r *Registry) AnalyzeFor(command string, implicit bool, platform ArgumentPlatform) Invocation {
	program, arguments, hasArguments := strings.Cut(command, " ")
	result := Invocation{Command: command, Program: program, Arguments: Arguments(arguments, len(program)+1), Status: Unknown, Input: "unknown"}
	name := ExecutableName(program)
	selected, prefix := -1, -1
	ambiguous := false
	for i, definition := range r.definitions {
		matches := slices.ContainsFunc(definition.Executables, func(candidate string) bool {
			return candidate == name || definition.IgnoreExecutableCase && strings.EqualFold(candidate, name)
		})
		if !matches || len(definition.Prefix) > len(result.Arguments) {
			continue
		}
		for j, token := range definition.Prefix {
			matches = matches && result.Arguments[j].Value == token
		}
		if matches && len(definition.Prefix) > prefix {
			selected, prefix = i, len(definition.Prefix)
			ambiguous = false
		} else if matches && len(definition.Prefix) == prefix {
			ambiguous = true
		}
	}
	if ambiguous {
		result.Reason = "multiple interpreter definitions match the same command"
		return result
	}
	if selected < 0 {
		result.Reason = "no interpreter definition matches the command"
		return result
	}
	definition := r.definitions[selected]
	// On Unix the runner performs CRT parsing itself. On Windows it passes a
	// command line to the executable; MSYS and native CRT programs can disagree.
	if platform != UnixArguments && strings.Contains(arguments, "'") {
		result.DefinitionID, result.Reason = definition.ID, "single-quote interpretation depends on the target executable's Windows argument parser"
		return result
	}
	args := result.Arguments[prefix:]
	if !hasArguments && prefix == 0 {
		defaults := definition.DefaultArguments
		if implicit {
			defaults = definition.ImplicitArguments
		}
		for _, value := range defaults {
			args = append(args, Token{Value: value, Start: 0, End: len(program)})
		}
	}
	return r.analyze(result, definition.ID, args, nil)
}

// Validate reuses a named provider without requiring its executable selector.
// Wrappers and sibling plugins can pass their forwarded arguments here.
func (r *Registry) Validate(definitionID string, arguments []Token) Invocation {
	return r.analyze(Invocation{Status: Unknown, Input: "unknown", Arguments: slices.Clone(arguments)}, definitionID, arguments, nil)
}

func (r *Registry) definition(id string, seen map[string]bool) (Definition, error) {
	if seen[id] {
		return Definition{}, fmt.Errorf("interpreter definition cycle at %q", id)
	}
	seen[id] = true
	for _, definition := range r.definitions {
		if definition.ID != id {
			continue
		}
		if definition.Extends != "" {
			base, err := r.definition(definition.Extends, seen)
			if err != nil {
				return Definition{}, err
			}
			if definition.Language == "" {
				definition.Language = base.Language
			}
			definition.Options = append(slices.Clone(base.Options), definition.Options...)
			definition.ToggleOptions = definition.ToggleOptions || base.ToggleOptions
			definition.LongOptionsFirst = definition.LongOptionsFirst || base.LongOptionsFirst
			definition.UnknownOptions = definition.UnknownOptions || base.UnknownOptions
		}
		return definition, nil
	}
	return Definition{}, fmt.Errorf("interpreter definition %q is not loaded", id)
}

func (r *Registry) analyze(result Invocation, id string, args []Token, chain []string) Invocation {
	if slices.Contains(chain, id) {
		result.Reason = "interpreter delegation cycle"
		return result
	}
	definition, err := r.definition(id, make(map[string]bool))
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	result.DefinitionID = id
	if definition.Delegate != "" {
		return r.analyze(result, definition.Delegate, args, append(chain, id))
	}
	result.Status, result.Language, result.Input = Valid, definition.Language, "none"
	shortSeen := false
	exitAfterLongOptions := false
	input := "file"
	fail := func(token Token, code, message string) {
		result.Status = Invalid
		result.Issues = append(result.Issues, Issue{Code: code, Message: message, Severity: "error", Start: token.Start, End: token.End})
	}
	for i := 0; i < len(args); i++ {
		token := args[i]
		arg := token.Value
		if exitAfterLongOptions && (!strings.HasPrefix(arg, "--") || arg == "--") {
			result.Input = "none"
			return result
		}
		if strings.Contains(arg, "${{") {
			result.Status, result.Reason, result.Input = Unknown, "interpreter arguments contain an unresolved expression", "unknown"
			return result
		}
		if arg == "--" {
			if i+1 < len(args) && args[i+1].Value == "{0}" {
				result.Input = input
			} else {
				result.Input = "argument"
			}
			return result
		}
		if len(arg) < 2 || arg[0] != '-' && (!definition.ToggleOptions || arg[0] != '+') {
			if arg == "{0}" {
				result.Input = input
			} else {
				result.Input = "argument"
			}
			return result
		}
		enabled, long := arg[0] == '-', strings.HasPrefix(arg, "--")
		if long && shortSeen && definition.LongOptionsFirst {
			fail(token, "option-order", fmt.Sprintf("long option %q must precede short options", arg))
			return result
		}
		if !long {
			shortSeen = true
		}
		flags := arg[1:]
		if long {
			flags = arg[2:]
		}
		for len(flags) > 0 {
			name := flags[:1]
			if long {
				name, flags = flags, ""
			} else {
				flags = flags[1:]
			}
			var option Option
			found := false
			for _, candidate := range slices.Backward(definition.Options) {
				if long && candidate.Long == name || !long && candidate.Short == name {
					option, found = candidate, true
					break
				}
			}
			if !found {
				if definition.UnknownOptions {
					result.Status, result.Reason, result.Input = Unknown, fmt.Sprintf("option %q has no validation definition", arg), "unknown"
					return result
				}
				fail(token, "unknown-option", fmt.Sprintf("unknown option %q for %s", arg, id))
				return result
			}
			value := option.Effect
			if option.Argument {
				i++
				if i == len(args) {
					fail(token, "missing-option-argument", fmt.Sprintf("option %q requires an argument", name))
					return result
				}
				argument := args[i]
				value = argument.Value
				if strings.Contains(value, "${{") {
					result.Status, result.Reason, result.Input = Unknown, "an interpreter option value is unresolved", "unknown"
					return result
				}
				if len(option.Choices) > 0 && !slices.Contains(option.Choices, value) {
					if definition.UnknownOptions {
						result.Status, result.Reason, result.Input = Unknown, "option value is not covered by the interpreter definition", "unknown"
						return result
					}
					fail(argument, "invalid-option-value", fmt.Sprintf("invalid %s option %q", option.Group, value))
					return result
				}
			}
			if option.ExitAfter == "option" {
				result.Input = "none"
				return result
			}
			if option.ExitAfter == "long-options" {
				exitAfterLongOptions = true
			}
			if option.Input != "" && enabled {
				input = option.Input
			}
			if option.Group != "" {
				result.Settings = append(result.Settings, Setting{Group: option.Group, Name: value, Enabled: enabled})
			}
			if option.Warning != "" {
				result.Issues = append(result.Issues, Issue{Code: "interpreter-option", Message: option.Warning, Severity: "warning", Start: token.Start, End: token.End})
			}
		}
	}
	return result
}

// CloneDefinitions detaches provider-owned option and selector slices.
func CloneDefinitions(definitions []Definition) []Definition {
	result := make([]Definition, len(definitions))
	for i, definition := range definitions {
		definition.Executables = slices.Clone(definition.Executables)
		definition.Prefix = slices.Clone(definition.Prefix)
		definition.DefaultArguments = slices.Clone(definition.DefaultArguments)
		definition.ImplicitArguments = slices.Clone(definition.ImplicitArguments)
		definition.Options = slices.Clone(definition.Options)
		for i := range definition.Options {
			definition.Options[i].Choices = slices.Clone(definition.Options[i].Choices)
		}
		result[i] = definition
	}
	return result
}
