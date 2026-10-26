package action

import (
	"strings"
	"sync"
	"unicode/utf8"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml"
)

// The cache belongs to parsed source metadata, never to an action invocation.
// Metadata views may copy its pointer; initialization and lazy mappings are synchronized.
type metadataPositionCache struct {
	mu      sync.Mutex
	scalars map[workflowlang.Pos]*yaml.Node
	sources map[workflowlang.Pos]*workflowlang.ScriptSource
	parser  *workflowlang.Parser
}

func (cache *metadataPositionCache) source(meta *Metadata, value *ExprString) *workflowlang.ScriptSource {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.scalars == nil {
		cache.scalars = make(map[workflowlang.Pos]*yaml.Node)
		cache.sources = make(map[workflowlang.Pos]*workflowlang.ScriptSource)
		cache.parser = workflowlang.NewSourceParser(meta.Source)
		cache.parser.IndexScalarSources(meta.Root)
		var visit func(*yaml.Node)
		visit = func(node *yaml.Node) {
			if node == nil {
				return
			}
			if node.Kind == yaml.ScalarNode {
				key := workflowlang.Pos{Line: node.Line, Col: node.Column}
				if _, exists := cache.scalars[key]; !exists {
					cache.scalars[key] = node
				}
			}
			// Anchors occur in Content; following Alias would revisit them.
			for _, child := range node.Content {
				visit(child)
			}
		}
		visit(meta.Root)
	}
	key := workflowlang.Pos{Line: value.Line, Col: value.Column}
	scalar := cache.scalars[key]
	if scalar == nil || scalar.Value != value.Value {
		return nil
	}
	if source, exists := cache.sources[key]; exists {
		return source
	}
	source := cache.parser.ScriptSource(scalar)
	cache.sources[key] = source
	return source
}

// ExpressionPosition maps a decoded expression offset to its YAML source position.
// If the offset cannot be mapped, it returns the start of the value.
func ExpressionPosition(meta *Metadata, value *ExprString, offset int) *workflowlang.Pos {
	fallback := &workflowlang.Pos{Line: value.Line, Col: value.Column}
	if offset <= 0 || offset > len(value.Value)+1 {
		return fallback
	}
	cache := meta.positions
	if cache == nil {
		return fallback
	}
	source := cache.source(meta, value)
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
