// Package document parses configuration formats and records source locations.
package document

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

type Location struct {
	File         string
	Line, Column int
}
type Document struct {
	Value          any
	Locations      map[string]Location
	ValueLocations map[string]Location
}

func Path(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
func Index(parent string, i int) string { return Path(parent, strconv.Itoa(i)) }
func location(file string, data []byte, offset int) Location {
	if offset < 0 {
		offset = 0
	}
	if offset > len(data) {
		offset = len(data)
	}
	prefix := data[:offset]
	return Location{file, 1 + bytes.Count(prefix, []byte{'\n'}), 1 + utf8.RuneCount(prefix[bytes.LastIndexByte(prefix, '\n')+1:])}
}

func newDocument(path string) Document {
	l := Location{path, 1, 1}
	return Document{Locations: map[string]Location{"": l}, ValueLocations: map[string]Location{"": l}}
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
