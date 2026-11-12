package mapping

import "strconv"

func ScalarString(value any) (string, bool) {
	switch value := value.(type) {
	case nil:
		return "", true
	case string:
		return value, true
	case bool:
		return strconv.FormatBool(value), true
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), true
	default:
		return "", false
	}
}
