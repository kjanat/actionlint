package codec

import (
	"errors"
	"fmt"
	"iter"
	"reflect"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "github.com/kjanat/go-yaml/v4"
	"github.com/kjanat/go-yaml/v4/plugin/limit"
)

// Session accounts for construction across one sequential schema traversal.
// Its source graph must not change before Close.
type Session struct {
	nodes                  map[*syntax.Node]*upstream.Node
	originals              map[*upstream.Node]*syntax.Node
	limits                 constructionLimits
	aliases, constructions int
	fatal                  *LoadError
	closed                 bool
}

type constructionLimits interface {
	CheckDepth(int, *upstream.DepthContext) error
	CheckAlias(int, int) error
}

// Cursor identifies a source occurrence together with its enclosing aliases.
type Cursor struct {
	session *Session
	node    *syntax.Node
	via     *aliasCrossing
	claim   *rootClaim
}

type aliasCrossing struct {
	node   *syntax.Node
	parent *aliasCrossing
}

// Entered is a resolved occurrence whose construction has already been counted.
type Entered struct {
	cursor Cursor
	claim  *rootClaim
}

type rootClaim struct{ decoded bool }

// Open creates a traversal without expanding aliases or constructing values.
func Open(root *syntax.Node) (*Session, Cursor, error) {
	graph, err := toParser(root)
	if err != nil {
		return nil, Cursor{}, err
	}
	s := &Session{nodes: make(map[*syntax.Node]*upstream.Node, len(graph.originals)), originals: graph.originals, limits: limit.New()}
	for raw, node := range graph.originals {
		s.nodes[node] = raw
	}
	return s, Cursor{session: s, node: root}, nil
}

// Close invalidates the session and every cursor derived from it.
func (s *Session) Close() { s.closed = true }

// Finish returns a fatal traversal error before any collected schema errors.
func (s *Session) Finish(err error) error {
	if s.fatal != nil {
		return s.fatal
	}
	return joinDecodeErrors(err)
}

// Wrap locates a schema error without discarding already located diagnostics.
func (s *Session) Wrap(n *syntax.Node, err error) error {
	if s.fatal != nil {
		return s.fatal
	}
	if err == nil {
		return nil
	}
	var mark Mark
	if n != nil {
		mark = Mark{Line: n.Line, Column: n.Column}
	}
	return joinDecodeErrors(locateDecodeErrors(err, mark))
}

// Node returns the original occurrence, which may be an alias.
func (c Cursor) Node() *syntax.Node { return c.node }

// Node returns the original concrete node reached by Enter.
func (e Entered) Node() *syntax.Node { return e.cursor.node }

func (c Cursor) check() error {
	var mark Mark
	if c.node != nil {
		mark = Mark{Line: c.node.Line, Column: c.node.Column}
	}
	if c.session == nil || c.session.closed || c.session.nodes == nil {
		return &GraphError{Mark: mark, Reason: "decoder is not active"}
	}
	if c.session.fatal != nil {
		return c.session.fatal
	}
	if _, ok := c.session.nodes[c.node]; !ok {
		return &GraphError{Mark: mark, Reason: "node belongs to another source graph"}
	}
	return nil
}

func (s *Session) fail(n *syntax.Node, stage upstream.Stage, err error) error {
	raw := upstream.NewLoadError(stage, err.Error(), upstream.Mark{Line: n.Line, Column: n.Column}, err)
	s.fatal = &LoadError{Message: raw.Message, Mark: Mark{Line: n.Line, Column: n.Column}, text: raw.Error(), cause: raw}
	return s.fatal
}

func (c Cursor) count() error {
	c.session.constructions++
	if c.via != nil {
		c.session.aliases++
	}
	if err := c.session.limits.CheckAlias(c.session.aliases, c.session.constructions); err != nil {
		return c.session.fail(c.node, upstream.ConstructorStage, err)
	}
	return nil
}

// Enter counts this occurrence and any document or alias nodes traversed to it.
// Unselected children are never expanded or counted.
func (c Cursor) Enter() (Entered, error) {
	for {
		if err := c.check(); err != nil {
			return Entered{}, err
		}
		if c.claim != nil && !c.claim.decoded {
			c.claim.decoded = true
		} else {
			if err := c.count(); err != nil {
				return Entered{}, err
			}
		}
		c.claim = nil
		switch c.node.Kind {
		case syntax.DocumentNode:
			if len(c.node.Content) == 0 {
				return Entered{cursor: c, claim: &rootClaim{}}, nil
			}
			c.node = c.node.Content[0]
		case syntax.AliasNode:
			for parent := c.via; parent != nil; parent = parent.parent {
				if parent.node == c.node {
					return Entered{}, c.session.fail(c.node, upstream.ComposerStage, fmt.Errorf("anchor '%s' value contains itself", c.node.Value))
				}
			}
			c.via = &aliasCrossing{node: c.node, parent: c.via}
			c.node = c.node.Alias
		case syntax.SequenceNode, syntax.MappingNode, syntax.ScalarNode:
			return Entered{cursor: c, claim: &rootClaim{}}, nil
		default:
			// Kind zero is the empty root node.
			return Entered{cursor: c, claim: &rootClaim{}}, nil
		}
	}
}

// Pairs yields mapping occurrences in source order, carrying enclosing aliases.
func (e Entered) Pairs() iter.Seq2[Cursor, Cursor] {
	return func(yield func(Cursor, Cursor) bool) {
		if e.cursor.node == nil || e.cursor.node.Kind != syntax.MappingNode {
			return
		}
		for i := 0; i+1 < len(e.cursor.node.Content); i += 2 {
			key, value := e.cursor, e.cursor
			key.node, value.node = e.cursor.node.Content[i], e.cursor.node.Content[i+1]
			if !yield(key, value) {
				return
			}
		}
	}
}

// Items yields sequence occurrences in source order, carrying enclosing aliases.
func (e Entered) Items() iter.Seq[Cursor] {
	return func(yield func(Cursor) bool) {
		if e.cursor.node == nil || e.cursor.node.Kind != syntax.SequenceNode {
			return
		}
		for _, node := range e.cursor.node.Content {
			child := e.cursor
			child.node = node
			if !yield(child) {
				return
			}
		}
	}
}

// Decode constructs a hook-free Go value using this occurrence's alias context.
func (c Cursor) Decode(target any) error { return c.decode(target, c.claim) }

// Decode uses the entered root's count once. Further decodes count it again.
func (e Entered) Decode(target any) error {
	return e.cursor.decode(target, e.claim)
}

func (c Cursor) decode(target any, claim *rootClaim) error {
	if err := c.check(); err != nil {
		return err
	}
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("YAML target must be a non-nil pointer")
	}
	if err := checkDecodeType(v.Elem().Type(), false, map[decodeShape]bool{}); err != nil {
		return err
	}
	return c.load(target, claim)
}

func (c Cursor) load(target any, claim *rootClaim, extra ...upstream.Option) error {
	var active []*upstream.Node
	for ancestor := c.via; ancestor != nil; ancestor = ancestor.parent {
		active = append(active, c.session.nodes[ancestor.node])
	}
	budget := &leafBudget{session: c.session, claim: claim}
	opts := append([]upstream.Option{upstream.WithV3Defaults(), upstream.WithPlugin(budget)}, extra...)
	err := translate(c.session.nodes[c.node].LoadWithAliasContext(target, active, opts...))
	if fatal, ok := FatalLoadError(err); ok && c.session.fatal == nil {
		c.session.fatal = fatal
	}
	return err
}

type decodeShape struct {
	typeOf   reflect.Type
	selected bool
}

func checkDecodeType(t reflect.Type, selected bool, seen map[decodeShape]bool) error {
	shape := decodeShape{t, selected}
	if seen[shape] {
		return nil
	}
	seen[shape] = true
	if t == reflect.TypeFor[Selected]() {
		if selected {
			return nil
		}
		return errors.New("YAML selections require a Select struct field")
	}
	if t == reflect.TypeFor[syntax.Node]() || t == reflect.TypeFor[upstream.Node]() {
		return fmt.Errorf("YAML node fields require an explicit schema traversal: %v", t)
	}
	if _, ok := t.MethodByName("UnmarshalYAML"); ok {
		return fmt.Errorf("YAML hooks require an explicit schema traversal: %v", t)
	}
	if t.Kind() != reflect.Pointer {
		if _, ok := reflect.PointerTo(t).MethodByName("UnmarshalYAML"); ok {
			return fmt.Errorf("YAML hooks require an explicit schema traversal: %v", t)
		}
	}
	// Only container kinds reach further types; every other kind is a leaf.
	kind := t.Kind()
	if kind == reflect.Pointer || kind == reflect.Slice || kind == reflect.Array {
		return checkDecodeType(t.Elem(), false, seen)
	}
	if kind == reflect.Map {
		if err := checkDecodeType(t.Key(), false, seen); err != nil {
			return err
		}
		return checkDecodeType(t.Elem(), false, seen)
	}
	if kind == reflect.Struct {
		for field := range t.Fields() {
			if (field.IsExported() || field.Anonymous) && field.Tag.Get("yaml") != "-" {
				if err := checkDecodeType(field.Type, selected, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type leafBudget struct {
	session                *Session
	claim                  *rootClaim
	aliases, constructions int
}

func (b *leafBudget) CheckDepth(depth int, ctx *upstream.DepthContext) error {
	return b.session.limits.CheckDepth(depth, ctx)
}

func (b *leafBudget) CheckAlias(aliases, constructions int) error {
	delta := constructions - b.constructions
	aliasDelta := aliases - b.aliases
	if b.claim != nil && !b.claim.decoded && b.constructions == 0 {
		delta--
		if aliasDelta > 0 {
			aliasDelta--
		}
		b.claim.decoded = true
	}
	b.aliases, b.constructions = aliases, constructions
	b.session.constructions += delta
	b.session.aliases += aliasDelta
	if delta == 0 && aliasDelta == 0 {
		return nil
	}
	return b.session.limits.CheckAlias(b.session.aliases, b.session.constructions)
}
