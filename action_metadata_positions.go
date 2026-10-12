package actionlint

import (
	"strings"
	"sync"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// The cache belongs to parsed source metadata, never to an action invocation.
// Metadata views may copy its pointer; initialization and lazy mappings are synchronized.
type metadataPositionCache struct {
	mu      sync.Mutex
	scalars map[Pos]*yaml.Node
	sources map[Pos]*scriptSource
	parser  *parser
}

func (cache *metadataPositionCache) source(meta *ActionMetadata, value *ActionExprString) *scriptSource {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.scalars == nil {
		cache.scalars = make(map[Pos]*yaml.Node)
		cache.sources = make(map[Pos]*scriptSource)
		cache.parser = newSourceParser(meta.src)
		cache.parser.indexScalarSources(meta.schemaRoot)
		var visit func(*yaml.Node)
		visit = func(node *yaml.Node) {
			if node == nil {
				return
			}
			if node.Kind == yaml.ScalarNode {
				key := Pos{Line: node.Line, Col: node.Column}
				if _, exists := cache.scalars[key]; !exists {
					cache.scalars[key] = node
				}
			}
			// Anchors occur in Content; following Alias would revisit them.
			for _, child := range node.Content {
				visit(child)
			}
		}
		visit(meta.schemaRoot)
	}
	key := Pos{Line: value.Line, Col: value.Column}
	scalar := cache.scalars[key]
	if scalar == nil || scalar.Value != value.Value {
		return nil
	}
	if source, exists := cache.sources[key]; exists {
		return source
	}
	source := cache.parser.scriptSource(scalar)
	cache.sources[key] = source
	return source
}

// Metadata diagnostics use the same verified scalar mappings as script analyzers.
// Missing source maps and folded expression strings retain the value position.
func metadataExpressionPosition(meta *ActionMetadata, value *ActionExprString, offset int) *Pos {
	fallback := &Pos{Line: value.Line, Col: value.Column}
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
		if pos, ok := source.endPos(line, column); ok {
			return pos
		}
		return fallback
	}
	if pos, ok := source.pos(line, column); ok {
		return pos
	}
	return fallback
}
