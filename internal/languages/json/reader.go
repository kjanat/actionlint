package json

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// FromJSONValue reads the first value using the runner's Newtonsoft.Json loose
// syntax. It deliberately does not consume trailing data. Object member
// collisions are exposed separately by JSONMemberCollisions; callers folding
// expressions must reject those because the selected member is mode-dependent.
// Valid JavaScript constructors return an unsupported-value error: their
// Newtonsoft string serialization is not modeled by constant evaluation.
func FromJSONValue(source string) (any, error) {
	value, _, err := Read(source)
	return value, err
}

type ReadError struct {
	Offset int
	Reason string
}

func (e *ReadError) Error() string { return e.Reason }

type fromJSONReader struct {
	source              string
	pos                 int
	Collisions          []MemberCollision
	constructors        int
	unsupported         bool
	IntegerNegativeZero bool
}

type ReadResult struct {
	Value               any
	Collisions          []MemberCollision
	IntegerNegativeZero bool
}

var ErrUnsupportedValue = errors.New("JSON constructor serialization is not supported by static evaluation")

func Read(source string) (any, []MemberCollision, error) {
	result, err := Parse(source)
	return result.Value, result.Collisions, err
}

func Parse(source string) (ReadResult, error) {
	r := fromJSONReader{source: source}
	value, err := r.value(0, true)
	if err == nil && r.unsupported {
		err = ErrUnsupportedValue
	}
	return ReadResult{Value: value, Collisions: r.Collisions, IntegerNegativeZero: r.IntegerNegativeZero}, err
}

func (r *fromJSONReader) fail(reason string) error {
	// EOF has no offending byte; retain encoding/json's consumed-byte offset.
	return &ReadError{min(r.pos+1, len(r.source)), reason}
}

func (r *fromJSONReader) char() byte {
	if r.pos == len(r.source) {
		return 0
	}
	return r.source[r.pos]
}

func (r *fromJSONReader) whitespace() {
	for r.pos < len(r.source) {
		c, size := utf8.DecodeRuneInString(r.source[r.pos:])
		// JsonTextReader skips embedded NUL characters between tokens.
		if c != 0 && !unicode.IsSpace(c) {
			return
		}
		r.pos += size
	}
}

func (r *fromJSONReader) comment() (string, error) {
	r.pos++
	kind := r.char()
	if kind != '/' && kind != '*' {
		return "", r.fail("invalid JSON comment")
	}
	r.pos++
	start := r.pos
	if kind == '/' {
		for r.pos < len(r.source) && r.char() != '\r' && r.char() != '\n' {
			r.pos++
		}
		return r.source[start:r.pos], nil
	}
	end := strings.Index(r.source[start:], "*/")
	if end < 0 {
		return "", r.fail("unterminated JSON comment")
	}
	r.pos += end + 2
	return r.source[start : start+end], nil
}

func (r *fromJSONReader) trivia() error {
	for {
		r.whitespace()
		if r.char() != '/' {
			return nil
		}
		if _, err := r.comment(); err != nil {
			return err
		}
	}
}

func (r *fromJSONReader) value(depth int, root bool) (any, error) {
	if root {
		r.whitespace()
		// JToken.ReadFrom exposes a leading comment as a string value.
		if r.char() == '/' {
			return r.comment()
		}
	} else if err := r.trivia(); err != nil {
		return nil, err
	}
	switch c := r.char(); c {
	case '{', '[':
		// JsonReader's default MaxDepth is 64, counting containers.
		if depth >= 64 {
			return nil, r.fail("JSON nesting exceeds maximum depth of 64")
		}
		if c == '{' {
			return r.object(depth + 1)
		}
		return r.array(depth+1, ']')
	case '\'', '"':
		return r.quoted()
	case ',':
		// Empty array/property values are JsonToken.Undefined, converted to
		// an empty string by ToPipelineContextData.
		return "", nil
	case 't':
		return r.literal("true", true)
	case 'f':
		return r.literal("false", false)
	case 'n':
		if strings.HasPrefix(r.source[r.pos:], "new") {
			return r.constructor(depth)
		}
		return r.literal("null", nil)
	case 'u':
		return r.literal("undefined", "")
	case 'N':
		return r.literal("NaN", math.NaN())
	case 'I':
		return r.literal("Infinity", math.Inf(1))
	case '-':
		if strings.HasPrefix(r.source[r.pos:], "-Infinity") {
			return r.literal("-Infinity", math.Inf(-1))
		}
		return r.number()
	case '.', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return r.number()
	default:
		return nil, r.fail("expected a JSON value")
	}
}

func (r *fromJSONReader) literal(word string, value any) (any, error) {
	if !strings.HasPrefix(r.source[r.pos:], word) {
		return nil, r.fail("invalid JSON literal")
	}
	r.pos += len(word)
	if r.pos < len(r.source) {
		c, _ := utf8.DecodeRuneInString(r.source[r.pos:])
		comment := strings.HasPrefix(r.source[r.pos:], "/*") || strings.HasPrefix(r.source[r.pos:], "//")
		if c != 0 && !unicode.IsSpace(c) && !strings.ContainsRune("}],", c) && !comment && (c != ')' || r.constructors == 0) {
			return nil, r.fail("invalid character after JSON literal")
		}
	}
	return value, nil
}

func (r *fromJSONReader) number() (any, error) {
	start := r.pos
	for r.pos < len(r.source) {
		c, size := utf8.DecodeRuneInString(r.source[r.pos:])
		if unicode.IsSpace(c) || c == 0 || strings.ContainsRune(",}]) /", c) {
			break
		}
		if !strings.ContainsRune("0123456789abcdefABCDEFxX.+-", c) {
			return nil, r.fail("invalid character in JSON number")
		}
		r.pos += size
	}
	text := r.source[start:r.pos]
	if len(text) > 1 && text[0] == '0' && !strings.ContainsRune(".eE", rune(text[1])) {
		base := 8
		if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
			base, text = 16, text[2:]
		}
		n, err := strconv.ParseUint(text, base, 64)
		if err != nil {
			return nil, r.fail("invalid non-decimal JSON integer")
		}
		// Convert.ToInt64(base) interprets the high bit as a sign bit.
		return float64(int64(n)), nil
	}
	if strings.ContainsAny(text, "xXabcdfABCDF_") {
		return nil, r.fail("invalid JSON number")
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil && !math.IsInf(n, 0) {
		return nil, r.fail("invalid JSON number")
	}
	if !strings.ContainsAny(text, ".eE") {
		if len(text) > 380 {
			return nil, r.fail("JSON integer exceeds maximum length of 380")
		}
		if n == 0 {
			r.IntegerNegativeZero = r.IntegerNegativeZero || math.Signbit(n)
			return float64(0), nil // Integer -0 loses its sign in JToken.
		}
		if integer, err := strconv.ParseInt(text, 10, 64); err == nil {
			return float64(integer), nil
		}
		integer, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return nil, r.fail("invalid JSON integer")
		}
		// Newtonsoft stores integers outside Int64 as BigInteger. Its .NET
		// conversion to double truncates the magnitude to 53 significant bits.
		truncated, _ := new(big.Float).SetPrec(53).SetMode(big.ToZero).SetInt(integer).Float64()
		return truncated, nil
	}
	return n, nil
}

func (r *fromJSONReader) quoted() (string, error) {
	quote := r.char()
	r.pos++
	var out strings.Builder
	for r.pos < len(r.source) {
		c := r.char()
		r.pos++
		if c == quote {
			return out.String(), nil
		}
		if c != '\\' {
			out.WriteByte(c)
			continue
		}
		c = r.char()
		if r.pos == len(r.source) {
			break
		}
		r.pos++
		switch c {
		case '"', '\'', '/', '\\':
			out.WriteByte(c)
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'u':
			first, err := r.hexRune()
			if err != nil {
				return "", err
			}
			for first >= 0xD800 && first <= 0xDBFF && strings.HasPrefix(r.source[r.pos:], `\u`) {
				r.pos += 2
				second, err := r.hexRune()
				if err != nil {
					return "", err
				}
				if second >= 0xDC00 && second <= 0xDFFF {
					first = utf16.DecodeRune(first, second)
					break
				}
				out.WriteRune(utf8.RuneError)
				first = second
			}
			if utf16.IsSurrogate(first) {
				first = utf8.RuneError
			}
			out.WriteRune(first)
		default:
			return "", r.fail("invalid JSON string escape")
		}
	}
	return "", r.fail("unterminated JSON string")
}

func (r *fromJSONReader) hexRune() (rune, error) {
	if len(r.source)-r.pos < 4 {
		return 0, r.fail("incomplete JSON Unicode escape")
	}
	n, err := strconv.ParseUint(r.source[r.pos:r.pos+4], 16, 16)
	if err != nil {
		return 0, r.fail("invalid JSON Unicode escape")
	}
	r.pos += 4
	return rune(n), nil
}

func (r *fromJSONReader) member() (string, error) {
	if r.char() == '\'' || r.char() == '"' {
		return r.quoted()
	}
	start := r.pos
	for r.pos < len(r.source) {
		c, size := utf8.DecodeRuneInString(r.source[r.pos:])
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '$' {
			break
		}
		r.pos += size
	}
	if start == r.pos {
		return "", r.fail("expected a JSON object member")
	}
	return r.source[start:r.pos], nil
}

func (r *fromJSONReader) object(depth int) (any, error) {
	r.pos++
	object := map[string]any{}
	seen := map[string]string{}
	for {
		if err := r.trivia(); err != nil {
			return nil, err
		}
		if r.char() == '}' {
			r.pos++
			return object, nil
		}
		key, err := r.member()
		if err != nil {
			return nil, err
		}
		folded := OrdinalIgnoreCaseKey(key)
		if first, exists := seen[folded]; exists {
			r.Collisions = append(r.Collisions, MemberCollision{first, key, int64(r.pos)})
		} else {
			seen[folded] = key
		}
		r.whitespace()
		if r.char() != ':' {
			return nil, r.fail("expected ':' after JSON object member")
		}
		r.pos++
		value, err := r.value(depth, false)
		if err != nil {
			return nil, err
		}
		object[key] = value
		if err := r.trivia(); err != nil {
			return nil, err
		}
		if r.char() == '}' {
			r.pos++
			return object, nil
		}
		if r.char() != ',' {
			return nil, r.fail("expected ',' or '}' after JSON object value")
		}
		r.pos++
	}
}

func (r *fromJSONReader) constructor(depth int) (any, error) {
	if depth >= 64 {
		return nil, r.fail("JSON nesting exceeds maximum depth of 64")
	}
	if _, err := r.literal("new", nil); err != nil {
		return nil, err
	}
	r.whitespace()
	for r.pos < len(r.source) {
		c, size := utf8.DecodeRuneInString(r.source[r.pos:])
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			break
		}
		r.pos += size
	}
	r.whitespace()
	if r.char() != '(' {
		return nil, r.fail("expected '(' after JSON constructor name")
	}
	r.constructors++
	_, err := r.array(depth+1, ')')
	r.constructors--
	r.unsupported = true
	return nil, err
}

func (r *fromJSONReader) array(depth int, closeChar byte) (any, error) {
	r.pos++
	array := []any{}
	for {
		if err := r.trivia(); err != nil {
			return nil, err
		}
		if r.char() == closeChar {
			r.pos++
			return array, nil
		}
		value, err := r.value(depth, false)
		if err != nil {
			return nil, err
		}
		array = append(array, value)
		if err := r.trivia(); err != nil {
			return nil, err
		}
		if r.char() == closeChar {
			r.pos++
			return array, nil
		}
		if r.char() != ',' {
			return nil, r.fail("expected ',' or ']' after JSON array value")
		}
		r.pos++
	}
}
