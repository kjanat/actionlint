package yamlscalar

import "regexp"

// The integer and float productions of the YAML 1.2 core schema, as GitHub implements them.
var (
	reCoreSchemaInt   = regexp.MustCompile(`^(?:[0-9]+|[-+][0-9]+|0x[0-9a-fA-F]+|0o[0-7]+)$`)
	reCoreSchemaFloat = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?$`)
)

// IsCoreSchemaBool recognizes the YAML 1.2 core boolean production.
// https://github.com/actions/runner/blob/258d6c857db3519913f7deb6004b60172f8043ae/src/Sdk/WorkflowParser/Conversion/YamlObjectReader.cs#L474-L717
func IsCoreSchemaBool(s string) bool {
	switch s {
	case "true", "True", "TRUE", "false", "False", "FALSE":
		return true
	}
	return false
}

func IsCoreSchemaNull(s string) bool {
	switch s {
	case "", "null", "Null", "NULL", "~":
		return true
	}
	return false
}

func IsCoreSchemaInt(s string) bool {
	return reCoreSchemaInt.MatchString(s)
}

func IsCoreSchemaFloat(s string) bool {
	switch s {
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF", "-.inf", "-.Inf", "-.INF", ".nan", ".NaN", ".NAN":
		return true
	}
	return reCoreSchemaFloat.MatchString(s)
}

// IsCoreSchemaNumber recognizes core integer and float productions.
// The YAML library resolves a plain scalar more permissively than the core schema, and the runner
// matches it against the integer production first and the float production second.
func IsCoreSchemaNumber(s string) bool {
	return IsCoreSchemaInt(s) || IsCoreSchemaFloat(s)
}

func IsCoreSchemaDecimal(s string) bool { return reCoreSchemaFloat.MatchString(s) }
