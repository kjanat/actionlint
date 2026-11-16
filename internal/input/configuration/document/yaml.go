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

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
)

func parseYAML(path string, data []byte) (*parsedSource, error) {
	d := newParsedSource(path)
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
		d.value = map[string]any{}
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
	d.value = value
	var walk func(*yaml.Node, string, map[*yaml.Node]bool)
	walk = func(n *yaml.Node, p string, active map[*yaml.Node]bool) {
		if n == nil || active[n] {
			return
		}
		active[n] = true
		defer delete(active, n)
		d.values[p] = sourcetext.Position{Line: n.Line, Column: n.Column}
		if n.Kind == yaml.AliasNode {
			walk(n.Alias, p, active)
			d.values[p] = sourcetext.Position{Line: n.Line, Column: n.Column}
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
						d.value = json.Number(integer.String())
					} else {
						setPath(d.value, p, json.Number(integer.String()))
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
			d.values[p] = sourcetext.Position{Line: n.Line, Column: n.Column}
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.ShortTag() == "!!merge" {
					continue
				}
				child := Path(p, k.Value)
				d.keys[child] = sourcetext.Position{Line: k.Line, Column: k.Column}
				walk(v, child, active)
			}
		case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode, yaml.StreamNode:
		case yaml.SequenceNode:
			for i, v := range n.Content {
				child := Index(p, i)
				d.keys[child] = sourcetext.Position{Line: v.Line, Column: v.Column}
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
