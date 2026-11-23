package actionmeta

import (
	"strings"
	"sync"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Provenance owns its source snapshot, so edits to Metadata never change what
// it reports. The zero value marks metadata without source provenance.
type Provenance struct{ parsed *parsedSource }

type parsedSource struct {
	text    string
	mu      sync.Mutex
	indexed bool
	scalars map[sourcetext.Position]*codec.Node
	parser  *parser.Parser
}

func newProvenance(text string) Provenance {
	return Provenance{parsed: &parsedSource{text: text}}
}

func (p Provenance) Text() (text string, ok bool) {
	if p.parsed == nil {
		return "", false
	}
	return p.parsed.text, true
}

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
	key := value.Position
	scalar := s.scalars[key]
	if scalar == nil || scalar.Value != value.Value {
		return nil
	}
	return s.parser.ScriptSource(scalar)
}

// index reparses the owned text. Callers may edit Metadata.Root after Parse,
// so index does not read it.
func (s *parsedSource) index() {
	s.indexed = true
	s.scalars = make(map[sourcetext.Position]*codec.Node)
	root, err := codec.ParseYAML([]byte(s.text))
	if err != nil {
		return
	}
	s.parser = parser.NewSourceParser([]byte(s.text))
	s.parser.IndexScalarSources(root)
	var visit func(*codec.Node)
	visit = func(node *codec.Node) {
		if node == nil {
			return
		}
		if node.Kind == codec.ScalarNode {
			key := sourcetext.Position{Line: node.Line, Column: node.Column}
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

// ExpressionPosition returns the start of the value when the offset cannot be mapped.
func ExpressionPosition(meta *Metadata, value *ExprString, offset int) *sourcetext.Position {
	fallback := new(value.Position)
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
