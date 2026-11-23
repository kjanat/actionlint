package commands

func NewWithShells(additional []Definition) (*Registry, error) {
	definitions := make([]Definition, 0, 4+len(additional))
	definitions = append(definitions, BashDefinition())
	for _, name := range []string{"sh", "dash", "ksh"} {
		definition := Definition{ID: name, Executables: []string{name, name + ".exe"}, IgnoreExecutableCase: true, Language: name, ToggleOptions: true, UnknownOptions: true}
		if name == "sh" {
			definition.DefaultArguments = []string{"-e", "{0}"}
			definition.ImplicitArguments = []string{"-e", "{0}"}
		}
		for _, flag := range []struct{ short, name string }{{"e", "errexit"}, {"u", "nounset"}, {"x", "xtrace"}, {"f", "noglob"}, {"v", "verbose"}, {"n", "noexec"}, {"a", "allexport"}, {"C", "noclobber"}} {
			definition.Options = append(definition.Options, Option{Short: flag.short, Group: "set", Effect: flag.name})
		}
		definition.Options = append(definition.Options, Option{Short: "c", Input: "command"}, Option{Short: "s", Input: "stdin"}, Option{Short: "o", Argument: true, Group: "set", Choices: []string{"errexit", "nounset", "xtrace", "noglob", "verbose", "noexec", "allexport", "noclobber"}})
		definitions = append(definitions, definition)
	}
	definitions = append(definitions, additional...)
	return New(definitions)
}

var builtinShells = func() *Registry {
	registry, err := NewWithShells(nil)
	if err != nil {
		panic(err)
	}
	return registry
}()

func BuiltinShells() *Registry { return builtinShells }
