// Package syntax defines YAML nodes independently of a parser implementation.
package syntax

type Kind uint32

const (
	DocumentNode Kind = 1 << iota
	SequenceNode
	MappingNode
	ScalarNode
	AliasNode
)

type Style uint32

const (
	TaggedStyle Style = 1 << iota
	DoubleQuotedStyle
	SingleQuotedStyle
	LiteralStyle
	FoldedStyle
	FlowStyle
)

// Node records resolved tags and one-based physical YAML coordinates. TaggedStyle
// marks an explicit tag; Alias points to its anchor within the same graph.
type Node struct {
	Kind                                  Kind
	Style                                 Style
	Tag, Value, Anchor                    string
	Alias                                 *Node
	Content                               []*Node
	HeadComment, LineComment, FootComment string
	Line, Column                          int
}

type nodeEncodingError struct{}

func (nodeEncodingError) Error() string {
	return "YAML syntax nodes require explicit graph encoding through codec"
}

// MarshalYAML prevents generic serializers from emitting Node's Go fields as data.
func (Node) MarshalYAML() (any, error) { return nil, nodeEncodingError{} }
