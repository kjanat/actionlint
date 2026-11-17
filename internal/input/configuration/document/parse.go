package document

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// syntax is the grammar of a configuration file, selected by its extension.
type syntax uint8

const (
	// yamlSyntax is selected for .yaml, .yml and any unrecognized extension.
	yamlSyntax syntax = iota
	// jsonSyntax is selected for .json and .jsonc; both accept comments and trailing commas.
	jsonSyntax
	// tomlSyntax is selected for .toml.
	tomlSyntax
)

// syntaxOf selects the grammar of path from its extension, ignoring a URL query and letter case.
func syntaxOf(path string) syntax {
	switch strings.ToLower(filepath.Ext(strings.SplitN(path, "?", 2)[0])) {
	case ".toml":
		return tomlSyntax
	case ".json", ".jsonc":
		return jsonSyntax
	default:
		return yamlSyntax
	}
}

// Parse reads one configuration document in the syntax selected by path.
// Every value is located in path.
func Parse(path string, data []byte) (*Node, error) {
	var parsed *parsedSource
	var err error
	switch syntaxOf(path) {
	case tomlSyntax:
		parsed, err = parseTOML(path, data)
	case jsonSyntax:
		parsed, err = parseJSON(path, data)
	default:
		parsed, err = parseYAML(path, data)
	}
	if err != nil {
		return nil, err
	}
	return parsed.node(), nil
}

// Marshal encodes value in the syntax selected by path without comments.
func Marshal(path string, value any) ([]byte, error) {
	switch syntaxOf(path) {
	case jsonSyntax:
		return json.MarshalIndent(value, "", "  ")
	case tomlSyntax:
		v, err := marshalValue(value, false)
		if err != nil {
			return nil, err
		}
		return toml.Marshal(v)
	default:
		v, err := marshalValue(value, true)
		if err != nil {
			return nil, err
		}
		return marshalYAML(v)
	}
}

func marshalValue(v any, yamlOutput bool) (any, error) {
	switch x := v.(type) {
	case json.Number:
		if !json.Valid([]byte(x)) {
			return nil, fmt.Errorf("invalid number %q", x)
		}
		if yamlOutput {
			return yamlNumber(x), nil
		}
		if !strings.ContainsAny(string(x), ".eE") {
			i, err := x.Int64()
			if err != nil {
				return nil, fmt.Errorf("TOML integer %q is outside the signed 64-bit range", x)
			}
			return i, nil
		}
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		if f, err := x.Float64(); err == nil {
			return f, nil
		}
		return nil, fmt.Errorf("cannot encode number %q", x)
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, a := range x {
			n, err := marshalValue(a, yamlOutput)
			if err != nil {
				return nil, err
			}
			m[k] = n
		}
		return m, nil
	case []any:
		a := make([]any, len(x))
		for i, item := range x {
			n, err := marshalValue(item, yamlOutput)
			if err != nil {
				return nil, err
			}
			a[i] = n
		}
		return a, nil
	default:
		return v, nil
	}
}
