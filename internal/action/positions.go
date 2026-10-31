package action

import (
	"strings"
	"sync"
	"unicode/utf8"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
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
	scalars map[workflowlang.Pos]*syntax.Node
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
func (p Provenance) source(value *ExprString) *workflowlang.ScriptSource {
	if p.parsed == nil {
		return nil
	}
	return p.parsed.source(value)
}

func (s *parsedSource) source(value *ExprString) *workflowlang.ScriptSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.indexed {
		s.index()
	}
	key := workflowlang.Pos{Line: value.Line, Col: value.Column}
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
	s.scalars = make(map[workflowlang.Pos]*syntax.Node)
	root, err := codec.Parse([]byte(s.text))
	if err != nil {
		return
	}
	s.parser = workflowparser.NewSourceParser([]byte(s.text))
	s.parser.IndexScalarSources(root)
	var visit func(*syntax.Node)
	visit = func(node *syntax.Node) {
		if node == nil {
			return
		}
		if node.Kind == syntax.ScalarNode {
			key := workflowlang.Pos{Line: node.Line, Col: node.Column}
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
func ExpressionPosition(meta *Metadata, value *ExprString, offset int) *workflowlang.Pos {
	fallback := &workflowlang.Pos{Line: value.Line, Col: value.Column}
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
