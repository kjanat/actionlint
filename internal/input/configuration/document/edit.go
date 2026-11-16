package document

import (
	"bytes"
	"slices"
)

// Editor rewrites one configuration file in its own syntax. Plugin
// declaration edits and configuration migrations both use it, so they keep
// comments, line endings and untouched settings in the same way.
type Editor struct {
	path     string
	syntax   syntax
	original []byte
	root     *Node
}

// NewEditor parses content as the file at path. The path's extension selects the syntax.
func NewEditor(path string, content []byte) (*Editor, error) {
	root, err := Parse(path, content)
	if err != nil {
		return nil, err
	}
	return &Editor{path: path, syntax: syntaxOf(path), original: slices.Clone(content), root: root}, nil
}

// Root returns the parsed file before any edit.
func (e *Editor) Root() *Node { return e.root }

// WritesNull reports whether the file's syntax can express null. TOML cannot.
func (e *Editor) WritesNull() bool { return e.syntax != tomlSyntax }

// TableFields lists the members of one list entry, in output order, for a
// TOML inline table or [[table]]. Every value is a string.
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

// lineEnding returns CRLF when content uses it anywhere, otherwise LF.
func lineEnding(content []byte) []byte {
	if bytes.Contains(content, []byte("\r\n")) {
		return []byte("\r\n")
	}
	return []byte("\n")
}

// splice returns original with original[start:end] replaced.
func splice(original []byte, start, end int, replacement []byte) []byte {
	var result []byte
	result = append(result, original[:start]...)
	result = append(result, replacement...)
	return append(result, original[end:]...)
}
