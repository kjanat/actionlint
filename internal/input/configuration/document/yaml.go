package document

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"slices"
	"strings"

	"github.com/kjanat/go-yaml/v4"
)

func parseYAML(path string, data []byte) (Document, error) {
	d := newDocument(path)
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil && !errors.Is(err, io.EOF) {
		return d, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return d, err
		}
		return d, errors.New("must contain one document")
	}
	if len(root.Content) == 0 {
		d.Value = map[string]any{}
		return d, nil
	}
	rootNode := root.Content[0]
	var value any
	if err := rootNode.Decode(&value); err != nil {
		return d, err
	}
	value, err := normalize(value)
	if err != nil {
		return d, err
	}
	d.Value = value
	var walk func(*yaml.Node, string, map[*yaml.Node]bool)
	walk = func(n *yaml.Node, p string, active map[*yaml.Node]bool) {
		if n == nil || active[n] {
			return
		}
		active[n] = true
		defer delete(active, n)
		d.ValueLocations[p] = Location{path, n.Line, n.Column}
		if n.Kind == yaml.AliasNode {
			walk(n.Alias, p, active)
			d.ValueLocations[p] = Location{path, n.Line, n.Column}
			return
		}
		if n.Kind == yaml.ScalarNode && (n.ShortTag() == "!!int" || n.ShortTag() == "!!float") {
			raw := strings.ReplaceAll(n.Value, "_", "")
			if !strings.Contains(raw, ".") {
				base := 10
				unsigned := strings.TrimLeft(raw, "+-")
				if strings.HasPrefix(unsigned, "0x") || strings.HasPrefix(unsigned, "0X") || strings.HasPrefix(unsigned, "0o") || strings.HasPrefix(unsigned, "0O") || strings.HasPrefix(unsigned, "0b") || strings.HasPrefix(unsigned, "0B") {
					base = 0
				}
				if integer, ok := new(big.Int).SetString(raw, base); ok {
					if p == "" {
						d.Value = json.Number(integer.String())
					} else {
						setPath(d.Value, p, json.Number(integer.String()))
					}
				}
			}
		}
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.ShortTag() != "!!merge" {
					continue
				}
				if v.Kind == yaml.SequenceNode {
					for _, v0 := range slices.Backward(v.Content) {
						walk(v0, p, active)
					}
				} else {
					walk(v, p, active)
				}
			}
			d.ValueLocations[p] = Location{path, n.Line, n.Column}
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.ShortTag() == "!!merge" {
					continue
				}
				child := Path(p, k.Value)
				d.Locations[child] = Location{path, k.Line, k.Column}
				walk(v, child, active)
			}
		case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode, yaml.StreamNode:
		case yaml.SequenceNode:
			for i, v := range n.Content {
				child := Index(p, i)
				d.Locations[child] = Location{path, v.Line, v.Column}
				walk(v, child, active)
			}
		}
	}
	walk(rootNode, "", map[*yaml.Node]bool{})
	return d, nil
}

func marshalYAML(value any) ([]byte, error) { return yaml.Marshal(value) }
func yamlNumber(number json.Number) any {
	tag := "!!int"
	if strings.ContainsAny(string(number), ".eE") {
		tag = "!!float"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(number)}
}

// RewriteYAML emits values while carrying comments from their original locations.
func RewriteYAML(original []byte, value any, locations map[string]Location) ([]byte, error) {
	var source yaml.Node
	if err := yaml.Unmarshal(original, &source); err != nil {
		return nil, err
	}
	encoded, err := marshalValue(value, true)
	if err != nil {
		return nil, err
	}
	var target yaml.Node
	if err := target.Encode(encoded); err != nil {
		return nil, err
	}
	type pair struct{ key, value *yaml.Node }
	type position struct{ line, column int }
	positions := map[position]pair{}
	var collect func(*yaml.Node)
	collect = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, val := node.Content[i], node.Content[i+1]
				p := pair{key, val}
				positions[position{key.Line, key.Column}] = p
				positions[position{val.Line, val.Column}] = p
				collect(val)
			}
		} else {
			for _, child := range node.Content {
				collect(child)
			}
		}
	}
	collect(&source)
	used := map[*yaml.Node]bool{}
	copyComments := func(dst, src *yaml.Node) {
		if src == nil || used[src] {
			return
		}
		used[src] = true
		dst.HeadComment = src.HeadComment
		dst.LineComment = src.LineComment
		dst.FootComment = src.FootComment
	}
	var apply func(*yaml.Node, string)
	apply = func(node *yaml.Node, path string) {
		switch node.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, val := node.Content[i], node.Content[i+1]
				child := Path(path, key.Value)
				if loc, ok := locations[child]; ok {
					p := positions[position{loc.Line, loc.Column}]
					copyComments(key, p.key)
					copyComments(val, p.value)
					if val.LineComment != "" && (val.Kind == yaml.MappingNode || val.Kind == yaml.SequenceNode) {
						if key.LineComment == "" {
							key.LineComment = val.LineComment
						} else {
							key.HeadComment = strings.TrimSpace(key.HeadComment + "\n" + val.LineComment)
						}
						val.LineComment = ""
					}
				}
				apply(val, child)
			}
		case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode, yaml.StreamNode:
		case yaml.SequenceNode:
			for i, child := range node.Content {
				apply(child, Index(path, i))
			}
		}
	}
	apply(&target, "")
	var unattached []string
	var leftovers func(*yaml.Node)
	leftovers = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if !used[node] {
			for _, comment := range []string{node.HeadComment, node.LineComment, node.FootComment} {
				if comment != "" {
					unattached = append(unattached, comment)
				}
			}
		}
		for _, child := range node.Content {
			leftovers(child)
		}
	}
	leftovers(&source)
	if len(unattached) > 0 {
		target.HeadComment = strings.Join(append(unattached, target.HeadComment), "\n")
	}
	return yaml.Marshal(&target)
}
