package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
	"github.com/pelletier/go-toml/v2/unstable"
)

func editTOMLPlugins(original []byte, sequence []any) ([]byte, error) {
	var values []string
	for _, entry := range sequence {
		switch value := entry.(type) {
		case string:
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			values = append(values, string(encoded))
		case map[string]any:
			var fields []string
			for _, key := range []string{"name", "version", "registry", "sha256"} {
				v, ok := value[key]
				if !ok {
					continue
				}
				encoded, err := json.Marshal(v)
				if err != nil {
					return nil, err
				}
				fields = append(fields, key+" = "+string(encoded))
			}
			values = append(values, "{ "+strings.Join(fields, ", ")+" }")
		default:
			return nil, errors.New("invalid plugin declaration")
		}
	}

	replacement := []byte("plugins = [" + strings.Join(values, ", ") + "]")
	parser := unstable.Parser{}
	parser.Reset(original)
	root := true
	pluginSection := -1
	var sections [][2]int
	for parser.NextExpression() {
		expression := parser.Expression()
		if expression.Kind == unstable.Table || expression.Kind == unstable.ArrayTable {
			root = false
			keys := expression.Key()
			if !keys.Next() {
				return nil, errors.New("TOML table has no key")
			}
			first := keys.Node()
			start := bytes.LastIndexByte(original[:int(first.Raw.Offset)], '\n') + 1
			if pluginSection >= 0 {
				sections = append(sections, [2]int{pluginSection, start})
				pluginSection = -1
			}
			if string(first.Data) == "plugins" {
				pluginSection = start
			}
		}
		if !root || expression.Kind != unstable.KeyValue {
			continue
		}
		keys := expression.Key()
		if keys.Next() && string(keys.Node().Data) == "plugins" && !keys.Next() {
			start := int(expression.Raw.Offset)
			return spliceManifest(original, start, start+int(expression.Raw.Length), replacement), nil
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	if pluginSection >= 0 {
		sections = append(sections, [2]int{pluginSection, len(original)})
	}
	for _, section := range slices.Backward(sections) {
		end := section[1]
		for end > section[0] {
			lineStart := bytes.LastIndexByte(original[:end-1], '\n') + 1
			line := bytes.TrimSpace(original[lineStart:end])
			if len(line) != 0 && line[0] != '#' {
				break
			}
			end = lineStart
		}
		original = spliceManifest(original, section[0], end, nil)
	}
	if len(sequence) != 0 {
		newline := "\n"
		if bytes.Contains(original, []byte("\r\n")) {
			newline = "\r\n"
		}
		tables, err := pluginTOMLTables(sequence, newline)
		if err != nil {
			return nil, err
		}
		if len(sections) != 0 {
			return spliceManifest(original, sections[0][0], sections[0][0], tables), nil
		}
		// A new array of tables is a root declaration even after other tables.
		// Appending a root-style plugins assignment here would change its scope.
		result := slices.Clone(original)
		if len(result) != 0 && !bytes.HasSuffix(result, []byte(newline)) {
			result = append(result, newline...)
		}
		if len(result) != 0 && !bytes.HasSuffix(result, []byte(newline+newline)) {
			result = append(result, newline...)
		}
		return append(result, tables...), nil
	}
	// Root assignments precede tables. Keep the schema preamble first, while
	// comments following it remain attached to their original setting or table.
	position := 0
	for offset := 0; offset < len(original); {
		end := bytes.IndexByte(original[offset:], '\n')
		if end < 0 {
			end = len(original)
		} else {
			end += offset + 1
		}
		line := bytes.TrimSpace(original[offset:end])
		if len(line) != 0 && line[0] != '#' {
			break
		}
		if bytes.HasPrefix(line, []byte("#:schema ")) || bytes.HasPrefix(line, []byte("#:schema\t")) {
			position = end
		} else if len(line) == 0 && position == offset && position != 0 {
			position = end
		}
		offset = end
	}
	newline := []byte{'\n'}
	if bytes.Contains(original, []byte("\r\n")) {
		newline = []byte("\r\n")
	}
	if position > 0 && original[position-1] != '\n' {
		replacement = append(slices.Clone(newline), replacement...)
	}
	replacement = append(replacement, newline...)
	return spliceManifest(original, position, position, replacement), nil
}

func pluginTOMLTables(sequence []any, newline string) ([]byte, error) {
	var tables []byte
	for index, entry := range sequence {
		var declarations config.PluginDeclarations
		if err := declarations.DecodeConfig([]any{entry}); err != nil {
			return nil, err
		}
		ref := declarations[0]
		if index != 0 {
			tables = append(tables, newline...)
		}
		tables = append(tables, "[[plugins]]"+newline...)
		var fields [][2]string
		if ref.DirectSource() != "" {
			fields = append(fields, [2]string{"source", ref.DirectSource()})
		} else {
			fields = append(fields, [2]string{"name", ref.Name()}, [2]string{"version", ref.Version()})
			if ref.Registry() != pluginref.DefaultRegistry {
				fields = append(fields, [2]string{"registry", ref.Registry()})
			}
			if digest := ref.CandidateDigest(); digest != "" {
				fields = append(fields, [2]string{"sha256", digest})
			}
		}
		for _, field := range fields {
			value, err := json.Marshal(field[1])
			if err != nil {
				return nil, err
			}
			tables = append(tables, field[0]+" = "...)
			tables = append(tables, value...)
			tables = append(tables, newline...)
		}
	}
	return tables, nil
}
