package actionlint

// Invocation describes a CLI operation independently of its argument parser.
type Invocation struct {
	Operation string
	Check     CheckRequest
	Render    RenderOptions
	JSON      bool
	Legacy    bool
	Origin    bool
	Rule      string
	Shell     string
}

// ConfigSelection selects one configuration file, repository discovery, or no file.
type ConfigSelection struct {
	Path     string
	Disabled bool
}

// CheckRequest contains analysis inputs. It carries no command-framework state.
type CheckRequest struct {
	Paths         []string
	Config        ConfigSelection
	StdinFilename string
	IgnoreRegex   []string
	ShellCheck    string
	Pyflakes      string
	Verbose       bool
	Debug         bool
}

// RenderOptions controls result presentation without changing analysis.
type RenderOptions struct {
	Format       OutputFormat
	Template     string
	TemplateFile string
	OutputFile   string
	Color        ColorOptionKind
	Oneline      bool
	Quiet        bool
	Summary      bool
}

func defaultInvocation() Invocation {
	return Invocation{
		Operation: "check",
		Check:     CheckRequest{StdinFilename: "<stdin>", ShellCheck: "shellcheck", Pyflakes: "pyflakes"},
	}
}
