package actionmeta

import (
	"strings"
	"sync"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Provenance is the parse origin of decoded metadata: the exact text Parse
// decoded and a lazily built index from decoded scalars back to that text.
// It owns its snapshot, so edits to a Metadata's exported fields never change
// what its provenance reports. Copies share one index. The zero value marks
// metadata without source provenance, such as catalog or JSON metadata.
type Provenance struct{ parsed *parsedSource }

// parsedSource is shared by every copy of a Provenance. The text is immutable;
// the index is built from it on first lookup.
type parsedSource struct {
	text    string
	mu      sync.Mutex
	indexed bool
	scalars map[source.Position]*codec.Node
	parser  *workflowparser.Parser
}

func newProvenance(text string) Provenance {
	return Provenance{parsed: &parsedSource{text: text}}
}

// Text returns the exact source decoded by Parse; ok is false without provenance.
func (p Provenance) Text() (text string, ok bool) {
	if p.parsed == nil {
		return "", false
	}
	return p.parsed.text, true
}

// source maps a decoded value to its original scalar. It returns nil without
// provenance or when no scalar at the value's position decodes to its text.
func (p Provenance) source(value *ExprString) *definition.ScriptSource {
	if p.parsed == nil {
		return nil
	}
	return p.parsed.source(value)
}

func (s *parsedSource) source(value *ExprString) *definition.ScriptSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.indexed {
		s.index()
	}
	key := source.Position{Line: value.Line, Column: value.Column}
	scalar := s.scalars[key]
	if scalar == nil || scalar.Value != value.Value {
		return nil
	}
	return s.parser.ScriptSource(scalar)
}

// index reparses the owned text rather than reading Metadata.Root, which
// callers may edit after Parse.
func (s *parsedSource) index() {
	s.indexed = true
	s.scalars = make(map[source.Position]*codec.Node)
	root, err := codec.ParseYAML([]byte(s.text))
	if err != nil {
		return
	}
	s.parser = workflowparser.NewSourceParser([]byte(s.text))
	s.parser.IndexScalarSources(root)
	var visit func(*codec.Node)
	visit = func(node *codec.Node) {
		if node == nil {
			return
		}
		if node.Kind == codec.ScalarNode {
			key := source.Position{Line: node.Line, Column: node.Column}
			if _, exists := s.scalars[key]; !exists {
				s.scalars[key] = node
			}
		}
		// Anchors occur in Content; following Alias would revisit them.
		for _, child := range node.Content {
			visit(child)
		}
	}
	visit(root)
}

// ExpressionPosition maps a decoded expression offset to its YAML source position.
// If the offset cannot be mapped, it returns the start of the value.
func ExpressionPosition(meta *Metadata, value *ExprString, offset int) *source.Position {
	fallback := &source.Position{Line: value.Line, Column: value.Column}
	if offset <= 0 || offset > len(value.Value)+1 {
		return fallback
	}
	source := meta.Provenance.source(value)
	prefix := value.Value[:offset-1]
	line := strings.Count(prefix, "\n") + 1
	column := utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
	if offset == len(value.Value)+1 {
		if pos, ok := source.EndPos(line, column); ok {
			return pos
		}
		return fallback
	}
	if pos, ok := source.Position(line, column); ok {
		return pos
	}
	return fallback
}
