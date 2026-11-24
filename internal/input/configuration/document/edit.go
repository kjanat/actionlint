package document

import (
	"bytes"
	"slices"
)

type Editor struct {
	path     string
	syntax   syntax
	original []byte
	root     *Node
}

func NewEditor(path string, content []byte) (*Editor, error) {
	root, err := ParseConfigDocument(path, content)
	if err != nil {
		return nil, err
	}
	return &Editor{path: path, syntax: syntaxOf(path), original: slices.Clone(content), root: root}, nil
}

func (e *Editor) Path() string { return e.path }

func (e *Editor) Root() *Node { return e.root }

func (e *Editor) Original() []byte { return slices.Clone(e.original) }

func (e *Editor) WritesNull() bool { return e.syntax != tomlSyntax }

type TableFields func(entry any) ([][2]string, error)

// SetList returns the file with the top-level member key set to entries and
// every other byte unchanged. Each entry is a string or an object. TOML writes
// object entries as inline tables and keeps a [[key]] array of tables in that
// form; fields orders the members of those tables.
func (e *Editor) SetList(key string, entries []any, fields TableFields) ([]byte, error) {
	switch e.syntax {
	case jsonSyntax:
		return setJSONList(e.original, key, entries)
	case tomlSyntax:
		return setTOMLList(e.original, key, entries, fields)
	default:
		return setYAMLList(e.original, key, entries)
	}
}

// Rewrite returns the file with its settings replaced by value. members lists
// the top-level members that may differ from the parsed file; the others must
// be unchanged. YAML moves each comment to the setting that origins locates
// at the comment's original key or value. TOML keeps every expression outside
// members and appends the new members after them. JSON is encoded again.
// The result ends with a newline and keeps CRLF line endings.
func (e *Editor) Rewrite(value any, members []string, origins map[string]Location) ([]byte, error) {
	var content []byte
	var err error
	switch e.syntax {
	case tomlSyntax:
		content, err = rewriteTOML(e.original, value, members)
	case jsonSyntax:
		content, err = Marshal(e.path, value)
	default:
		content, err = rewriteYAML(e.original, value, origins)
	}
	if err != nil {
		return nil, err
	}
	if !bytes.HasSuffix(content, []byte("\n")) {
		content = append(content, '\n')
	}
	if e.syntax != tomlSyntax && bytes.Contains(e.original, []byte("\r\n")) {
		content = bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n"))
	}
	return content, nil
}

func lineEnding(content []byte) []byte {
	if bytes.Contains(content, []byte("\r\n")) {
		return []byte("\r\n")
	}
	return []byte("\n")
}

func splice(original []byte, start, end int, replacement []byte) []byte {
	var result []byte
	result = append(result, original[:start]...)
	result = append(result, replacement...)
	return append(result, original[end:]...)
}
