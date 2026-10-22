package expression

// findingKind binds a production site to a configurable rule and a stable code.
// Wording, source location and execution backend do not determine identity.
type findingKind struct{ rule, code string }

var (
	syntax                 = findingKind{"expression-syntax", "invalid-syntax"}
	unknownVariable        = findingKind{"expression-reference", "unknown-variable"}
	unknownProperty        = findingKind{"expression-reference", "unknown-property"}
	unknownFunction        = findingKind{"expression-reference", "unknown-function"}
	unavailableContext     = findingKind{"context-availability", "unavailable-context"}
	unavailableFunction    = findingKind{"context-availability", "unavailable-function"}
	missingCall            = findingKind{"expression-call", "missing-call"}
	argumentCount          = findingKind{"expression-call", "argument-count"}
	formatPlaceholder      = findingKind{"expression-call", "format-placeholder"}
	invalidJSON            = findingKind{"expression-call", "invalid-json"}
	jsonCollision          = findingKind{"expression-call", "json-member-collision"}
	argumentType           = findingKind{"expression-type", "argument-type"}
	operandType            = findingKind{"expression-type", "operand-type"}
	invalidVariableName    = findingKind{"expression-reference", "invalid-variable-name"}
	unknownConfigVariable  = findingKind{"expression-reference", "unknown-config-variable"}
	unknownSecret          = findingKind{"expression-reference", "unknown-secret"}
	unknownEventProperty   = findingKind{"event-payload", "unknown-property"}
	untrustedInterpolation = findingKind{"script-injection", "untrusted-interpolation"}
)
