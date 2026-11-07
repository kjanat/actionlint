// Package interpreters registers language-specific invocation definitions with
// the shared command model. Plugin definitions use the same registry.
package interpreters

import (
	"actionlint.kjanat.dev/v2/internal/languages/bash"
	"actionlint.kjanat.dev/v2/internal/model/commands"
)

func New(additional []commands.Definition) (*commands.Registry, error) {
	definitions := []commands.Definition{bash.Definition()}
	for _, name := range []string{"sh", "dash", "ksh"} {
		definition := commands.Definition{ID: name, Executables: []string{name, name + ".exe"}, IgnoreExecutableCase: true, Language: name, ToggleOptions: true, UnknownOptions: true}
		if name == "sh" {
			definition.DefaultArguments = []string{"-e", "{0}"}
			definition.ImplicitArguments = []string{"-e", "{0}"}
		}
		for _, flag := range []struct{ short, name string }{{"e", "errexit"}, {"u", "nounset"}, {"x", "xtrace"}, {"f", "noglob"}, {"v", "verbose"}, {"n", "noexec"}, {"a", "allexport"}, {"C", "noclobber"}} {
			definition.Options = append(definition.Options, commands.Option{Short: flag.short, Group: "set", Effect: flag.name})
		}
		definition.Options = append(definition.Options, commands.Option{Short: "c", Input: "command"}, commands.Option{Short: "s", Input: "stdin"}, commands.Option{Short: "o", Argument: true, Group: "set", Choices: []string{"errexit", "nounset", "xtrace", "noglob", "verbose", "noexec", "allexport", "noclobber"}})
		definitions = append(definitions, definition)
	}
	definitions = append(definitions, additional...)
	return commands.New(definitions)
}

var builtin = func() *commands.Registry {
	registry, err := New(nil)
	if err != nil {
		panic(err)
	}
	return registry
}()

func Builtin() *commands.Registry { return builtin }
