package codec

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

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

// CoreSchemaNumber converts a core integer or float scalar to the number the
// runner reads from it. Infinities and NaN keep their YAML meaning, and 0x and
// 0o integers pass through the runner's signed 32-bit representation. It
// reports false for text outside those productions and for decimals beyond
// the float64 range.
func CoreSchemaNumber(s string) (float64, bool) {
	if !IsCoreSchemaNumber(s) {
		return 0, false
	}
	switch strings.ToLower(s) {
	case ".inf", "+.inf":
		return math.Inf(1), true
	case "-.inf":
		return math.Inf(-1), true
	case ".nan":
		return math.NaN(), true
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0o") {
		n, err := CoreSchemaRadixInt(s)
		return float64(n), err == nil
	}
	n, err := strconv.ParseFloat(s, 64)
	return n, err == nil
}

// CoreSchemaRadixInt converts a 0x hexadecimal or 0o octal core integer the
// way the runner does: through an unsigned 32-bit value read as a signed
// two's-complement integer, so 0xffffffff is -1. Values wider than 32 bits are
// errors.
func CoreSchemaRadixInt(s string) (int, error) {
	base := 0
	switch {
	case strings.HasPrefix(s, "0x"):
		base = 16
	case strings.HasPrefix(s, "0o"):
		base = 8
	}
	if base == 0 || !IsCoreSchemaInt(s) {
		return 0, errors.New("invalid YAML integer")
	}
	n, err := strconv.ParseUint(s[2:], base, 32)
	if err != nil {
		return 0, err
	}
	// Decode the two's-complement representation without narrowing an
	// unsigned value into a signed type. Both branches fit a 32-bit int.
	if n <= math.MaxInt32 {
		return int(n), nil
	}
	return -1 - int(math.MaxUint32-n), nil
}
