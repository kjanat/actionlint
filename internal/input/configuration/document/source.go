package document

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

// parsedSource collects a decoded value and the positions of its keys and
// values while one file is parsed. Parse turns it into a Node.
type parsedSource struct {
	file   string
	value  any
	keys   map[string]sourcetext.Position
	values map[string]sourcetext.Position
}

func newParsedSource(file string) *parsedSource {
	start := sourcetext.Position{Line: 1, Column: 1}
	return &parsedSource{file: file, keys: map[string]sourcetext.Position{"": start}, values: map[string]sourcetext.Position{"": start}}
}

// node locates every pointer that has a value position. Key positions without
// a value, such as the intermediate keys of a dotted TOML key, are dropped.
func (p *parsedSource) node() *Node {
	locations := make(map[string]Location, len(p.values))
	for pointer, value := range p.values {
		key := p.keys[pointer]
		locations[pointer] = Location{File: p.file, Value: value, Key: key}
	}
	return Capture(p.value, locations)
}

// normalize keeps the same scalar and container types across formats.
func normalize(v any) (any, error) {
	switch x := v.(type) {
	case nil, string, bool, json.Number:
		return x, nil
	case int:
		return json.Number(strconv.FormatInt(int64(x), 10)), nil
	case int64:
		return json.Number(strconv.FormatInt(x, 10)), nil
	case uint64:
		return json.Number(strconv.FormatUint(x, 10)), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return x, nil
		}
		return json.Number(strconv.FormatFloat(x, 'g', -1, 64)), nil
	case time.Time:
		return x.Format(time.RFC3339Nano), nil
	case toml.LocalDate:
		return x.String(), nil
	case toml.LocalTime:
		return x.String(), nil
	case toml.LocalDateTime:
		return x.String(), nil
	case map[string]any:
		for k, item := range x {
			n, err := normalize(item)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			x[k] = n
		}
		return x, nil
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, item := range x {
			s, ok := k.(string)
			if !ok {
				return nil, errors.New("mapping key must be a string")
			}
			n, err := normalize(item)
			if err != nil {
				return nil, err
			}
			m[s] = n
		}
		return m, nil
	case []any:
		for i, item := range x {
			n, err := normalize(item)
			if err != nil {
				return nil, err
			}
			x[i] = n
		}
		return x, nil
	default:
		return nil, fmt.Errorf("unsupported configuration value %s", reflect.TypeOf(v))
	}
}

func setPath(root any, path string, value any) {
	if path == "" {
		return
	}
	parts := strings.Split(path[1:], "/")
	current := root
	for i, encoded := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		switch v := current.(type) {
		case map[string]any:
			if last {
				if _, ok := v[key]; ok {
					v[key] = value
				}
				return
			}
			current = v[key]
		case []any:
			n, err := strconv.Atoi(key)
			if err != nil || n < 0 || n >= len(v) {
				return
			}
			if last {
				v[n] = value
				return
			}
			current = v[n]
		default:
			return
		}
	}
}
