package actionlint

// invocation describes a CLI operation independently of its argument parser.
type invocation struct {
	Operation operation
	Check     checkInvocation
	Render    renderOptions
	JSON      bool
	Legacy    bool
	Origin    bool
	Rule      string
	Shell     string
}

// configSelection selects one configuration file, repository discovery, or no file.
type configSelection struct {
	Path     string
	Disabled bool
}

// checkInvocation contains analysis inputs. It carries no command-framework state.
type checkInvocation struct {
	Paths         []string
	Config        configSelection
	StdinFilename string
	IgnoreRegex   []string
	ShellCheck    string
	Pyflakes      string
	Verbose       bool
	Debug         bool
}

// renderOptions controls result presentation without changing analysis.
type renderOptions struct {
	Format       OutputFormat
	Template     string
	TemplateFile string
	OutputFile   string
	Color        ColorOptionKind
	Oneline      bool
	Quiet        bool
	Summary      bool
}

func defaultInvocation() invocation {
	return invocation{
		Operation: "check",
		Check:     checkInvocation{StdinFilename: "<stdin>", ShellCheck: "shellcheck", Pyflakes: "pyflakes"},
	}
}

// operation is selected by a parser; execution rejects values outside this set.
type operation string

const (
	operationCheck          operation = "check"
	operationVersion        operation = "version"
	operationRules          operation = "rules"
	operationDoctor         operation = "doctor"
	operationConfigPath     operation = "config path"
	operationConfigShow     operation = "config show"
	operationConfigValidate operation = "config validate"
	operationConfigInit     operation = "config init"
	operationCompletion     operation = "completion"
)
