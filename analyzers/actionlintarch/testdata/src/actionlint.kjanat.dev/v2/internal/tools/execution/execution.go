package execution // want package:"project dependency paths"
import "actionlint.kjanat.dev/v2/analyzer"

type Scope struct {
	Caller   string // want "ALA104"
	Call     int    // want "ALA104"
	Depth    int    // want "ALA104"
	Document string // want "ALA104"
}

func runtime() analyzer.Runtime { return analyzer.Runtime{Shell: "sh"} }
