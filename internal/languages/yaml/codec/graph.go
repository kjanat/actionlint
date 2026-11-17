package codec

import (
	"errors"
	"reflect"
)

func validateGraph(root *Node) error {
	active, done := map[*Node]bool{}, map[*Node]bool{}
	pending := []*Node{root}
	var visit func(*Node) error
	visit = func(node *Node) error {
		if node == nil {
			return errors.New("YAML graph contains a nil node")
		}
		if active[node] {
			return errors.New("YAML content cycle must use an alias")
		}
		if done[node] {
			return nil
		}
		if _, err := parserKind(node.Kind); err != nil {
			return err
		}
		if _, err := parserStyle(node.Style); err != nil {
			return err
		}
		if node.Kind == 0 && (node != root || !reflect.ValueOf(*node).IsZero()) {
			return errors.New("YAML zero kind is only valid for an empty root")
		}
		if node.Kind != AliasNode && node.Alias != nil {
			return errors.New("YAML alias target on a non-alias node")
		}
		switch node.Kind {
		case AliasNode:
			if node.Alias == nil || node.Alias.Kind == AliasNode || node.Alias.Kind == 0 {
				return errors.New("YAML alias requires a concrete target")
			}
			if len(node.Content) != 0 {
				return errors.New("YAML alias cannot have content")
			}
		case ScalarNode:
			if len(node.Content) != 0 {
				return errors.New("YAML scalar cannot have content")
			}
		case MappingNode:
			if len(node.Content)%2 != 0 {
				return errors.New("YAML mapping requires key-value pairs")
			}
		case DocumentNode:
			if len(node.Content) > 1 {
				return errors.New("YAML document requires at most one root")
			}
		case SequenceNode:
		}
		active[node] = true
		for _, child := range node.Content {
			if err := visit(child); err != nil {
				return err
			}
		}
		if node.Kind == AliasNode {
			pending = append(pending, node.Alias)
		}
		delete(active, node)
		done[node] = true
		return nil
	}
	for len(pending) != 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if err := visit(node); err != nil {
			return err
		}
	}
	return nil
}

func validateEncodingGraph(root *Node) error {
	if err := validateGraph(root); err != nil {
		return err
	}
	anchors := map[string]*Node{}
	var visit func(*Node) error
	visit = func(node *Node) error {
		if node.Kind == AliasNode {
			if node.Value == "" || node.Value != node.Alias.Anchor || anchors[node.Value] != node.Alias {
				return errors.New("YAML alias must reference its previously emitted anchor")
			}
			return nil
		}
		if node.Anchor != "" {
			anchors[node.Anchor] = node
		}
		for _, child := range node.Content {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root)
}
