package tree

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/config/address"

	"github.com/kjanat/go-yaml/v4"
)

// Loaded is an expanded config graph and its dependency/provenance data.
type Loaded struct {
	Node    *yaml.Node
	Files   []string
	Origins map[*yaml.Node]string
}

// Load resolves extends relative to their declaring file or HTTPS address. The
// caller supplies all bytes and validates each document before merging.
func Load(path string, read func(string) ([]byte, error), normalize func(string, *yaml.Node) (*yaml.Node, error)) (Loaded, error) {
	return LoadWithMerge(path, read, normalize, Merge)
}

// LoadWithMerge lets a document owner define which mappings are atomic values.
// The tree loader retains ownership of cycle detection and source provenance.
func LoadWithMerge(path string, read func(string) ([]byte, error), normalize func(string, *yaml.Node) (*yaml.Node, error), merge func(*yaml.Node, *yaml.Node, map[*yaml.Node]Input) *yaml.Node) (Loaded, error) {
	result := Loaded{Origins: map[*yaml.Node]string{}}
	inputs := map[*yaml.Node]Input{}
	active := map[string]bool{}
	cache := map[string]*yaml.Node{}
	var load func(string, int) (*yaml.Node, error)
	load = func(path string, depth int) (*yaml.Node, error) {
		readPath := path
		path, err := address.Normalize(path)
		if err != nil {
			return nil, err
		}
		// Use physical identity when available; virtual readers still use clean paths.
		identity := path
		if !address.Remote(path) {
			if actual, err := filepath.EvalSymlinks(path); err == nil {
				identity = actual
			}
		}
		if active[identity] {
			return nil, fmt.Errorf("cyclic config extends at %q", path)
		}
		if depth > 32 {
			return nil, fmt.Errorf("config extends exceeds 32 levels at %q", path)
		}
		if n, ok := cache[path]; ok {
			return n, nil
		}
		active[identity] = true
		defer delete(active, identity)
		data, err := read(readPath)
		if err != nil {
			return nil, fmt.Errorf("read config %q: %w", path, err)
		}
		node, err := Decode(readPath, data)
		if err != nil {
			return nil, fmt.Errorf("config %q: %w", path, err)
		}
		node, err = Expand(node, map[*yaml.Node]bool{})
		if err != nil {
			return nil, err
		}
		if normalize != nil {
			node, err = normalize(path, node)
			if err != nil {
				return nil, fmt.Errorf("config %q: %w", path, err)
			}
		}
		var bases []string
		if node.Kind == yaml.MappingNode {
			for i := 0; i < len(node.Content); i += 2 {
				if node.Content[i].Value == "extends" {
					for _, entry := range node.Content[i+1].Content {
						if entry.Kind != yaml.ScalarNode || entry.ShortTag() != "!!str" {
							return nil, fmt.Errorf("config %q extends entries must be strings", path)
						}
					}
					if err := node.Content[i+1].Decode(&bases); err != nil {
						return nil, fmt.Errorf("config %q extends: %w", path, err)
					}
				}
			}
		}
		var base *yaml.Node
		for _, name := range bases {
			name, err := address.Resolve(path, name)
			if err != nil {
				return nil, fmt.Errorf("extends in %q: %w", path, err)
			}
			n, err := load(name, depth+1)
			if err != nil {
				// A missing base invalidates this graph, rather than making the
				// selected leaf eligible for repository-discovery fallback.
				return nil, extendsError{origin: path, detail: err.Error(), canceled: errors.Is(err, context.Canceled), deadline: errors.Is(err, context.DeadlineExceeded)}
			}
			base = merge(base, n, inputs)
		}
		var mark func(*yaml.Node)
		mark = func(n *yaml.Node) {
			result.Origins[n] = path
			inputs[n] = Input{File: path}
			for _, child := range n.Content {
				mark(child)
			}
		}
		mark(node)
		merged := merge(base, node, inputs)
		if len(bases) == 0 && merged.Kind == yaml.MappingNode {
			leaf := *merged
			leaf.Content = append([]*yaml.Node{}, merged.Content...)
			for i := 0; i < len(leaf.Content); i += 2 {
				if leaf.Content[i].Value == "extends" {
					leaf.Content = append(leaf.Content[:i], leaf.Content[i+2:]...)
					break
				}
			}
			inputs[&leaf] = inputs[merged]
			merged = &leaf
		}
		cache[path] = merged
		result.Files = append(result.Files, path)
		return merged, nil
	}
	node, err := load(path, 0)
	for node, input := range inputs {
		result.Origins[node] = input.File
	}
	result.Node = node
	return result, err
}

type extendsError struct {
	origin, detail     string
	canceled, deadline bool
}

func (e extendsError) Error() string { return fmt.Sprintf("extends in %q: %s", e.origin, e.detail) }
func (e extendsError) Is(target error) bool {
	return e.canceled && target == context.Canceled || e.deadline && target == context.DeadlineExceeded
}
