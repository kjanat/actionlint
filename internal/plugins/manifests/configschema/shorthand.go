package configschema

// Shorthand is a key written directly under tools.<name> that sets one
// property of the provider's configuration. Manifests declare shorthands in
// config_shorthands, keyed by the shorthand's name.
type Shorthand struct {
	// Path names the configuration schema property the shorthand sets, one
	// property name per nesting level, such as ["lint", "select"].
	Path []string `json:"path"`
	// Precedence decides which value is kept when the same mapping sets both
	// the shorthand and the property under tools.<name>.config.
	Precedence Precedence `json:"precedence"`
}

// Precedence picks the value kept when a shorthand and the property it sets
// are both written. A null shorthand never replaces a value.
type Precedence string

const (
	// PrecedenceShorthand keeps the shorthand's value.
	PrecedenceShorthand Precedence = "shorthand"
	// PrecedenceConfig keeps the value written under tools.<name>.config.
	PrecedenceConfig Precedence = "config"
)
