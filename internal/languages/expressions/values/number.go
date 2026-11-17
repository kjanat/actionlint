package values

import (
	"math"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

func ToNumber(value any) float64 {
	switch value := value.(type) {
	case nil:
		return 0
	case bool:
		if value {
			return 1
		}
		return 0
	case float64:
		return value
	case string:
		value = strings.TrimSpace(value)
		switch strings.ToLower(value) {
		case "":
			return 0
		case "infinity", "+infinity":
			return math.Inf(1)
		case "-infinity":
			return math.Inf(-1)
		}
		if codec.IsCoreSchemaDecimal(value) {
			if number, err := strconv.ParseFloat(value, 64); err == nil || math.IsInf(number, 0) {
				return number
			}
		} else if number, err := codec.CoreSchemaRadixInt(value); err == nil {
			// ExpressionUtility.ParseNumber uses signed 32-bit hexadecimal/octal conversion.
			return float64(number)
		}
	}
	return math.NaN()
}
