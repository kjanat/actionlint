package codec

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// https://github.com/actions/runner/blob/258d6c857db3519913f7deb6004b60172f8043ae/src/Sdk/WorkflowParser/Conversion/YamlObjectReader.cs#L474-L717
var (
	reCoreSchemaInt   = regexp.MustCompile(`^(?:[0-9]+|[-+][0-9]+|0x[0-9a-fA-F]+|0o[0-7]+)$`)
	reCoreSchemaFloat = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?$`)
)

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

func IsCoreSchemaNumber(s string) bool {
	return IsCoreSchemaInt(s) || IsCoreSchemaFloat(s)
}

func IsCoreSchemaDecimal(s string) bool { return reCoreSchemaFloat.MatchString(s) }

// CoreSchemaNumber converts a core integer or float scalar to the number the
// runner reads from it.
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
// way the runner does. The runner reads an unsigned 32-bit value as a signed
// two's-complement integer, and 0xffffffff is -1. Values wider than 32 bits are
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
	if n <= math.MaxInt32 {
		return int(n), nil
	}
	return -1 - int(math.MaxUint32-n), nil
}
