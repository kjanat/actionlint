package toolsettings

type Shorthand struct {
	Path       []string   `json:"path"`
	Precedence Precedence `json:"precedence"`
}

// Precedence never lets a null shorthand replace a value.
type Precedence string

const (
	PrecedenceShorthand Precedence = "shorthand"
	PrecedenceConfig    Precedence = "config"
)
