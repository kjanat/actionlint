package configtree

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Loaded is an expanded local config graph and its dependency/provenance data.
type Loaded struct {
	Node    *yaml.Node
	Files   []string
	Origins map[*yaml.Node]string
}

// Load resolves local extends paths relative to their declaring file. The caller
// validates/normalizes each document before merging. No URLs or commands execute.
func Load(path string, read func(string) ([]byte, error), normalize func(string, *yaml.Node) (*yaml.Node, error)) (Loaded, error) {
	result := Loaded{Origins: map[*yaml.Node]string{}}
	inputs := map[*yaml.Node]Input{}
	active := map[string]bool{}
	cache := map[string]*yaml.Node{}
	var load func(string, int) (*yaml.Node, error)
	load = func(path string, depth int) (*yaml.Node, error) {
		path, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path = filepath.Clean(path)
		// Use physical identity when available; virtual readers still use clean paths.
		identity := path
		if actual, err := filepath.EvalSymlinks(path); err == nil {
			identity = actual
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
		data, err := read(path)
		if err != nil {
			return nil, fmt.Errorf("read config %q: %w", path, err)
		}
		var document yaml.Node
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		if err := decoder.Decode(&document); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("config %q: %w", path, err)
		}
		var extra yaml.Node
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("config %q must contain one document", path)
		}
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if len(document.Content) > 0 {
			node = document.Content[0]
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
			if strings.TrimSpace(name) == "" || strings.Contains(name, "://") || strings.ContainsRune(name, 0) {
				return nil, fmt.Errorf("extends in %q requires nonempty local file paths", path)
			}
			if !filepath.IsAbs(name) {
				name = filepath.Join(filepath.Dir(path), name)
			}
			n, err := load(name, depth+1)
			if err != nil {
				// A missing base is a broken config, not an absent optional root config.
				return nil, fmt.Errorf("extends in %q: %v", path, err) //nolint:errorlint // Do not propagate os.ErrNotExist from inherited files.
			}
			base = Merge(base, n, inputs)
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
		merged := Merge(base, node, inputs)
		// Retain the declaring leaf's extends list, never an inherited parent's list.
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
