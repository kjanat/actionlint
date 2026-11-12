package security

import (
	"fmt"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

// UntrustedInputMap is a node in a tree of untrusted input paths. Roots name
// contexts; children name properties or array elements.
type UntrustedInputMap struct {
	Name     string
	Parent   *UntrustedInputMap
	Children map[string]*UntrustedInputMap
}

func (m *UntrustedInputMap) String() string {
	var b strings.Builder
	m.buildPath(&b)
	return b.String()
}

// findChild finds the child with the exact key.
func (m *UntrustedInputMap) findChild(key string) (*UntrustedInputMap, bool) {
	if m != nil && m.Children != nil {
		if c, ok := m.Children[key]; ok {
			return c, true
		}
	}
	return nil, false
}

// findObjectProp finds a property by name. A `**` child matches any property name.
func (m *UntrustedInputMap) findObjectProp(name string) (*UntrustedInputMap, bool) {
	if c, ok := m.findChild(name); ok {
		return c, true
	}
	return m.findAnyProp()
}

// findArrayElem finds the child representing an array element.
func (m *UntrustedInputMap) findArrayElem() (*UntrustedInputMap, bool) {
	return m.findChild("*")
}

// findAnyProp finds the child that matches any property name.
func (m *UntrustedInputMap) findAnyProp() (*UntrustedInputMap, bool) {
	return m.findChild("**")
}

// buildPath follows parents to build a path such as `github.event.commits.*.body`.
func (m *UntrustedInputMap) buildPath(b *strings.Builder) {
	if m.Parent != nil && m.Parent.Name != "" {
		m.Parent.buildPath(b)
		b.WriteRune('.')
	}
	b.WriteString(m.Name)
}

// NewUntrustedInputMap creates a node in the untrusted input search tree.
// The name `*` matches an array element and is reached by an index access or an object
// filter. The name `**` matches any property name. A map which uses either name must have
// it as its only child.
func NewUntrustedInputMap(name string, children ...*UntrustedInputMap) *UntrustedInputMap {
	m := &UntrustedInputMap{
		Name:     name,
		Parent:   nil,
		Children: nil, // Leaf of the tree is nil
	}
	if len(children) > 0 {
		m.Children = make(map[string]*UntrustedInputMap, len(children))
		for _, c := range children {
			c.Parent = m
			m.Children[c.Name] = c
		}
	}
	return m
}

// UntrustedInputSearchRoots maps context names to trees of untrusted input paths.
// The checker follows those paths through properties, array indices and object filters.
type UntrustedInputSearchRoots map[string]*UntrustedInputMap

// AddRoot adds a context root to the search tree.
func (ms UntrustedInputSearchRoots) AddRoot(m *UntrustedInputMap) {
	ms[m.Name] = m
}

// TODO: Automatically generate BuiltinUntrustedInputs from https://github.com/github/codeql/blob/main/javascript/ql/src/experimental/Security/CWE-094/ExpressionInjection.ql

// BuiltinUntrustedInputs lists inputs treated as untrusted in `run:` scripts.
// See these references for details.
// - https://securitylab.github.com/research/github-actions-untrusted-input/
// - https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks
// - https://github.com/github/codeql/blob/main/javascript/ql/src/experimental/Security/CWE-094/ExpressionInjection.ql
var BuiltinUntrustedInputs = UntrustedInputSearchRoots{
	"github": NewUntrustedInputMap("github",
		NewUntrustedInputMap("event",
			NewUntrustedInputMap("issue",
				NewUntrustedInputMap("title"),
				NewUntrustedInputMap("body"),
			),
			NewUntrustedInputMap("pull_request",
				NewUntrustedInputMap("title"),
				NewUntrustedInputMap("body"),
				NewUntrustedInputMap("head",
					NewUntrustedInputMap("ref"),
					NewUntrustedInputMap("label"),
					NewUntrustedInputMap("repo",
						NewUntrustedInputMap("default_branch"),
					),
				),
			),
			NewUntrustedInputMap("comment",
				NewUntrustedInputMap("body"),
			),
			NewUntrustedInputMap("review",
				NewUntrustedInputMap("body"),
			),
			NewUntrustedInputMap("review_comment",
				NewUntrustedInputMap("body"),
			),
			NewUntrustedInputMap("pages",
				NewUntrustedInputMap("*",
					NewUntrustedInputMap("page_name"),
				),
			),
			NewUntrustedInputMap("commits",
				NewUntrustedInputMap("*",
					NewUntrustedInputMap("message"),
					NewUntrustedInputMap("committer",
						NewUntrustedInputMap("email"),
						NewUntrustedInputMap("name"),
					),
					NewUntrustedInputMap("author",
						NewUntrustedInputMap("email"),
						NewUntrustedInputMap("name"),
					),
				),
			),
			NewUntrustedInputMap("head_commit",
				NewUntrustedInputMap("message"),
				NewUntrustedInputMap("committer",
					NewUntrustedInputMap("email"),
					NewUntrustedInputMap("name"),
				),
				NewUntrustedInputMap("author",
					NewUntrustedInputMap("email"),
					NewUntrustedInputMap("name"),
				),
			),
			NewUntrustedInputMap("discussion",
				NewUntrustedInputMap("title"),
				NewUntrustedInputMap("body"),
			),
			NewUntrustedInputMap("workflow",
				NewUntrustedInputMap("name"),
				NewUntrustedInputMap("path"),
			),
			// A workflow_run can describe a run from a fork. Repository metadata,
			// branch names and Git commit identities/messages can be contributor-controlled.
			NewUntrustedInputMap("workflow_run",
				NewUntrustedInputMap("actor", NewUntrustedInputMap("name"), NewUntrustedInputMap("email")),
				NewUntrustedInputMap("triggering_actor", NewUntrustedInputMap("name"), NewUntrustedInputMap("email")),
				NewUntrustedInputMap("head_repository",
					NewUntrustedInputMap("default_branch"),
					NewUntrustedInputMap("description"),
					NewUntrustedInputMap("homepage"),
					NewUntrustedInputMap("owner", NewUntrustedInputMap("name"), NewUntrustedInputMap("email")),
				),
				NewUntrustedInputMap("head_branch"),
				NewUntrustedInputMap("name"),
				NewUntrustedInputMap("path"),
				NewUntrustedInputMap("referenced_workflows",
					NewUntrustedInputMap("*",
						NewUntrustedInputMap("ref"),
						NewUntrustedInputMap("path"),
					),
				),
				NewUntrustedInputMap("pull_requests",
					NewUntrustedInputMap("*",
						NewUntrustedInputMap("head", NewUntrustedInputMap("ref")),
					),
				),
				NewUntrustedInputMap("display_title"),
				NewUntrustedInputMap("head_commit",
					NewUntrustedInputMap("message"),
					NewUntrustedInputMap("author", NewUntrustedInputMap("name"), NewUntrustedInputMap("email")),
					NewUntrustedInputMap("committer", NewUntrustedInputMap("name"), NewUntrustedInputMap("email")),
				),
			),
		),
		NewUntrustedInputMap("head_ref"),
	),
}

// UntrustedInputChecker is a checker to detect untrusted inputs in an expression syntax tree.
// This checker checks object property accesses, array index accesses, and object filters. And
// detects paths to untrusted inputs. Found errors are stored in this instance and can be get via
// Errs method.
//
// Note: To avoid breaking the state of checking property accesses on nested property accesses like
// foo[aaa.bbb].bar, IndexAccessNode.Index must be visited before IndexAccessNode.Operand.
type UntrustedInputChecker struct {
	roots           UntrustedInputSearchRoots
	filteringObject bool
	cur             []*UntrustedInputMap
	start           exprsyntax.Node
	errs            []*exprlang.Error
	safeCalls       int
	booleanContexts int
	visits          []untrustedInputVisit
}

type untrustedInputVisit struct {
	node    exprsyntax.Node
	boolean bool
}

// NewUntrustedInputChecker creates a new UntrustedInputChecker instance. The roots argument is a
// search tree which defines untrusted input paths as trees.
func NewUntrustedInputChecker(roots UntrustedInputSearchRoots) *UntrustedInputChecker {
	return &UntrustedInputChecker{
		roots: roots,
		errs:  []*exprlang.Error{},
	}
}

// Reset the state for next search
func (u *UntrustedInputChecker) reset() {
	u.start = nil
	u.filteringObject = false
	u.cur = u.cur[:0]
}

func (u *UntrustedInputChecker) compact() {
	delta := 0
	for i, c := range u.cur {
		if c == nil {
			delta++
			continue
		}
		if delta > 0 {
			u.cur[i-delta] = c
		}
	}
	u.cur = u.cur[:len(u.cur)-delta]
}

func (u *UntrustedInputChecker) onVar(v *exprsyntax.VariableNode) {
	c, ok := u.roots[v.Name] // Find root context (currently only "github" exists)
	if !ok {
		return
	}
	u.start = v
	u.cur = append(u.cur, c)
}

func (u *UntrustedInputChecker) onPropAccess(name string) {
	compact := false
	for i, cur := range u.cur {
		c, ok := cur.findObjectProp(name)
		if !ok {
			u.cur[i] = nil
			compact = true
			continue
		}
		u.cur[i] = c // depth + 1
	}
	if compact {
		u.compact()
	}
}

func (u *UntrustedInputChecker) onIndexAccess() {
	if u.filteringObject {
		u.filteringObject = false
		return // For example, match `github.event.*.body[0]` as `github.event.commits[0].body`
	}

	compact := false
	for i, cur := range u.cur {
		if c, ok := cur.findArrayElem(); ok {
			u.cur[i] = c
			continue
		}
		if c, ok := cur.findAnyProp(); ok {
			u.cur[i] = c
			continue
		}
		u.cur[i] = nil
		compact = true
	}
	if compact {
		u.compact()
	}
}

func (u *UntrustedInputChecker) onObjectFilter() {
	u.filteringObject = true

	compact := false
	for i, cur := range u.cur {
		// Object filter for arrays
		if c, ok := cur.findArrayElem(); ok {
			u.cur[i] = c
			continue
		}

		if len(cur.Children) == 0 {
			u.cur[i] = nil
			compact = true
		}

		// Object filter for objects
		first := true
		for _, c := range cur.Children {
			if first {
				u.cur[i] = c
				first = false
			} else {
				u.cur = append(u.cur, c)
			}
		}
	}
	if compact {
		u.compact()
	}
}

func (u *UntrustedInputChecker) end() {
	u.endInput(false)
}

func (u *UntrustedInputChecker) endInput(serialized bool) {
	var inputs []string
	for _, cur := range u.cur {
		if cur.Children != nil && !serialized {
			continue // When `Children` is nil, the node is a leaf
		}
		var b strings.Builder
		cur.buildPath(&b)
		inputs = append(inputs, b.String())
	}
	u.reportInputs(u.start, inputs)
	u.reset()
}

func (u *UntrustedInputChecker) reportInputs(start exprsyntax.Node, inputs []string) {
	if len(inputs) == 1 {
		err := scriptInjectionIssuef("untrusted-interpolation",
			start,
			"%q is potentially untrusted. avoid using it directly in inline scripts. instead, pass it through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details",
			inputs[0],
		)
		u.errs = append(u.errs, err)
	} else if len(inputs) > 1 {
		// When multiple untrusted inputs are detected, it means the expression extracts multiple properties with object
		// filter syntax. Show all properties in error message.
		err := scriptInjectionIssuef("untrusted-interpolation",
			start,
			"object filter extracts potentially untrusted properties %s. avoid using the value directly in inline scripts. instead, pass the value through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details",
			messages.SortedQuotes(inputs),
		)
		u.errs = append(u.errs, err)
	}
}

// serializedContainers follows values selected for JSON serialization. Boolean
// operands and function predicates do not contribute container contents.
func (u *UntrustedInputChecker) serializedContainers(expr exprsyntax.Node) {
	switch n := expr.(type) {
	case *exprsyntax.LogicalOpNode:
		if value, known := expreval.ConditionConstantValue(n.Left); known {
			if exprvalues.ExpressionTruthy(value) != (n.Kind == exprsyntax.LogicalOpNodeKindOr) {
				u.serializedContainers(n.Right)
			}
			return
		}
		if n.Kind == exprsyntax.LogicalOpNodeKindOr {
			u.serializedContainers(n.Left)
		}
		u.serializedContainers(n.Right)
	case *exprsyntax.FuncCallNode:
		if !strings.EqualFold(n.Callee, "case") || len(n.Args) < 3 || len(n.Args)%2 != 1 {
			return
		}
		for i := 0; i < len(n.Args)-1; i += 2 {
			if value, known := expreval.ConditionConstantValue(n.Args[i]); known {
				if !exprvalues.ExpressionTruthy(value) {
					continue
				}
				u.serializedContainers(n.Args[i+1])
				return
			}
			u.serializedContainers(n.Args[i+1])
		}
		u.serializedContainers(n.Args[len(n.Args)-1])
	default:
		access := &UntrustedInputChecker{roots: u.roots}
		if !access.followAccess(expr) {
			return
		}
		var inputs []string
		for _, cur := range access.cur {
			if cur.Children != nil {
				inputs = append(inputs, cur.String())
			}
		}
		u.reportInputs(access.start, inputs)
	}
}

func (u *UntrustedInputChecker) followAccess(expr exprsyntax.Node) bool {
	switch n := expr.(type) {
	case *exprsyntax.VariableNode:
		u.onVar(n)
	case *exprsyntax.ObjectDerefNode:
		if !u.followAccess(n.Receiver) {
			return false
		}
		u.onPropAccess(n.Property)
	case *exprsyntax.ArrayDerefNode:
		if !u.followAccess(n.Receiver) {
			return false
		}
		u.onObjectFilter()
	case *exprsyntax.IndexAccessNode:
		if !u.followAccess(n.Operand) {
			return false
		}
		if key, ok := n.Index.(*exprsyntax.StringNode); ok {
			u.onPropAccess(strings.ToLower(key.Value))
		} else {
			u.onIndexAccess()
		}
	default:
		return false
	}
	if u.start != nil {
		u.start = expr
	}
	return true
}

func (u *UntrustedInputChecker) OnVisitNodeEnter(n exprsyntax.Node) {
	boolean := false
	switch n.(type) {
	case *exprsyntax.CompareOpNode, *exprsyntax.NotOpNode:
		boolean = true
	}
	if len(u.visits) > 0 {
		switch parent := u.visits[len(u.visits)-1].node.(type) {
		case *exprsyntax.LogicalOpNode:
			boolean = boolean || parent.Kind == exprsyntax.LogicalOpNodeKindAnd && parent.Left == n
		case *exprsyntax.FuncCallNode:
			if strings.EqualFold(parent.Callee, "case") {
				for i := 0; i < len(parent.Args)-1; i += 2 {
					boolean = boolean || parent.Args[i] == n
				}
			}
		}
	}
	u.visits = append(u.visits, untrustedInputVisit{node: n, boolean: boolean})
	if boolean {
		u.booleanContexts++
	}
	if f, ok := n.(*exprsyntax.FuncCallNode); ok && exprfunctions.IsStringPredicate(f.Callee) {
		u.safeCalls++
	}
}

// OnVisitNodeLeave is a callback which should be called on visiting node after visiting its children.
func (u *UntrustedInputChecker) OnVisitNodeLeave(n exprsyntax.Node) {
	defer func() {
		if len(u.visits) > 0 {
			visit := u.visits[len(u.visits)-1]
			u.visits = u.visits[:len(u.visits)-1]
			if visit.boolean {
				u.booleanContexts--
			}
		}
	}()
	// Skip unsafe checks if we are inside of safe function call expression
	if u.safeCalls > 0 {
		if f, ok := n.(*exprsyntax.FuncCallNode); ok && exprfunctions.IsStringPredicate(f.Callee) {
			u.safeCalls--
		}
		return
	}

	switch n := n.(type) {
	case *exprsyntax.VariableNode:
		u.end()
		u.onVar(n)
	case *exprsyntax.ObjectDerefNode:
		u.onPropAccess(n.Property)
		if u.start != nil {
			u.start = n
		}
	case *exprsyntax.IndexAccessNode:
		if lit, ok := n.Index.(*exprsyntax.StringNode); ok {
			// Special case like github['event']['issue']['title']
			u.onPropAccess(strings.ToLower(lit.Value))
		} else {
			u.onIndexAccess()
		}
		if u.start != nil {
			u.start = n
		}
	case *exprsyntax.ArrayDerefNode:
		u.onObjectFilter()
		if u.start != nil {
			u.start = n
		}
	case *exprsyntax.FuncCallNode:
		if u.booleanContexts == 0 && strings.EqualFold(n.Callee, "toJSON") && len(n.Args) == 1 {
			switch n.Args[0].(type) {
			case *exprsyntax.VariableNode, *exprsyntax.ObjectDerefNode, *exprsyntax.ArrayDerefNode, *exprsyntax.IndexAccessNode:
				u.endInput(true)
				return
			default:
				u.serializedContainers(n.Args[0])
			}
		}
		u.end()
	default:
		u.end()
	}
}

// OnVisitEnd is a callback which should be called after visiting whole syntax tree. This callback
// is necessary to handle the case where an untrusted input access is at root of expression.
func (u *UntrustedInputChecker) OnVisitEnd() {
	u.end()
}

// Errs returns errors detected by this checker. This method should be called after visiting all
// nodes in a syntax tree.
func (u *UntrustedInputChecker) Errs() []*exprlang.Error {
	out := make([]*exprlang.Error, len(u.errs))
	for i, issue := range u.errs {
		copied := *issue
		out[i] = &copied
	}
	return out
}

// Init initializes a state of checker.
func (u *UntrustedInputChecker) Init() {
	u.errs = u.errs[:0]
	u.safeCalls = 0
	u.booleanContexts = 0
	u.visits = u.visits[:0]
	u.reset()
}

func scriptInjectionIssuef(code string, node exprsyntax.Node, format string, args ...any) *exprlang.Error {
	span := exprsyntax.ExpressionSpan(node)
	return &exprlang.Error{Rule: "script-injection", Code: code, Message: fmt.Sprintf(format, args...), Offset: span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column}
}

func checkUntrustedInputs(expr exprsyntax.Node) []*exprlang.Error {
	checker := NewUntrustedInputChecker(BuiltinUntrustedInputs)
	exprsyntax.Visit(expr, func(node, _ exprsyntax.Node, entering bool) {
		if entering {
			checker.OnVisitNodeEnter(node)
		} else {
			checker.OnVisitNodeLeave(node)
		}
	})
	checker.OnVisitEnd()
	return checker.Errs()
}
