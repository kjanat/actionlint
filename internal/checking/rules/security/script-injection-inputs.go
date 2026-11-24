package security

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/messages"
)

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

func (m *UntrustedInputMap) findChild(key string) (*UntrustedInputMap, bool) {
	if m != nil && m.Children != nil {
		if c, ok := m.Children[key]; ok {
			return c, true
		}
	}
	return nil, false
}

func (m *UntrustedInputMap) findObjectProp(name string) (*UntrustedInputMap, bool) {
	if c, ok := m.findChild(name); ok {
		return c, true
	}
	return m.findAnyProp()
}

func (m *UntrustedInputMap) findArrayElem() (*UntrustedInputMap, bool) {
	return m.findChild("*")
}

func (m *UntrustedInputMap) findAnyProp() (*UntrustedInputMap, bool) {
	return m.findChild("**")
}

func (m *UntrustedInputMap) buildPath(b *strings.Builder) {
	if m.Parent != nil && m.Parent.Name != "" {
		m.Parent.buildPath(b)
		b.WriteRune('.')
	}
	b.WriteString(m.Name)
}

// NewUntrustedInputMap treats the name `*` as an array element and `**` as any
// property name. A map which uses either name must have it as its only child.
func NewUntrustedInputMap(name string, children ...*UntrustedInputMap) *UntrustedInputMap {
	m := &UntrustedInputMap{
		Name:     name,
		Parent:   nil,
		Children: nil,
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

type UntrustedInputSearchRoots map[string]*UntrustedInputMap

func (ms UntrustedInputSearchRoots) AddRoot(m *UntrustedInputMap) {
	ms[m.Name] = m
}

// BuiltinUntrustedInputs lists inputs treated as untrusted in `run:` scripts.
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

// UntrustedInputChecker needs IndexAccessNode.Index visited before
// IndexAccessNode.Operand for nested accesses such as foo[aaa.bbb].bar.
type UntrustedInputChecker struct {
	roots           UntrustedInputSearchRoots
	filteringObject bool
	cur             []*UntrustedInputMap
	start           syntax.Node
	errs            []*typecheck.Error
	safeCalls       int
	booleanContexts int
	visits          []untrustedInputVisit
}

type untrustedInputVisit struct {
	node    syntax.Node
	boolean bool
}

func NewUntrustedInputChecker(roots UntrustedInputSearchRoots) *UntrustedInputChecker {
	return &UntrustedInputChecker{
		roots: roots,
		errs:  []*typecheck.Error{},
	}
}

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

func (u *UntrustedInputChecker) onVar(v *syntax.VariableNode) {
	c, ok := u.roots[v.Name]
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
		u.cur[i] = c
	}
	if compact {
		u.compact()
	}
}

func (u *UntrustedInputChecker) onIndexAccess() {
	if u.filteringObject {
		u.filteringObject = false
		return
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
		if c, ok := cur.findArrayElem(); ok {
			u.cur[i] = c
			continue
		}

		if len(cur.Children) == 0 {
			u.cur[i] = nil
			compact = true
		}

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
			continue
		}
		var b strings.Builder
		cur.buildPath(&b)
		inputs = append(inputs, b.String())
	}
	u.reportInputs(u.start, inputs)
	u.reset()
}

func (u *UntrustedInputChecker) reportInputs(start syntax.Node, inputs []string) {
	if len(inputs) == 1 {
		err := scriptInjectionIssuef("untrusted-interpolation",
			start,
			"%q is potentially untrusted. avoid using it directly in inline scripts. instead, pass it through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details",
			inputs[0],
		)
		u.errs = append(u.errs, err)
	} else if len(inputs) > 1 {
		err := scriptInjectionIssuef("untrusted-interpolation",
			start,
			"object filter extracts potentially untrusted properties %s. avoid using the value directly in inline scripts. instead, pass the value through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details",
			messages.SortedQuotes(inputs),
		)
		u.errs = append(u.errs, err)
	}
}

func (u *UntrustedInputChecker) serializedContainers(expr syntax.Node) {
	switch n := expr.(type) {
	case *syntax.LogicalOpNode:
		if value, known := evaluate.ConditionConstantValue(n.Left); known {
			if exprvalues.ExpressionTruthy(value) != (n.Kind == syntax.LogicalOpNodeKindOr) {
				u.serializedContainers(n.Right)
			}
			return
		}
		if n.Kind == syntax.LogicalOpNodeKindOr {
			u.serializedContainers(n.Left)
		}
		u.serializedContainers(n.Right)
	case *syntax.FuncCallNode:
		if !strings.EqualFold(n.Callee, "case") || len(n.Args) < 3 || len(n.Args)%2 != 1 {
			return
		}
		for i := 0; i < len(n.Args)-1; i += 2 {
			if value, known := evaluate.ConditionConstantValue(n.Args[i]); known {
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

func (u *UntrustedInputChecker) followAccess(expr syntax.Node) bool {
	switch n := expr.(type) {
	case *syntax.VariableNode:
		u.onVar(n)
	case *syntax.ObjectDerefNode:
		if !u.followAccess(n.Receiver) {
			return false
		}
		u.onPropAccess(n.Property)
	case *syntax.ArrayDerefNode:
		if !u.followAccess(n.Receiver) {
			return false
		}
		u.onObjectFilter()
	case *syntax.IndexAccessNode:
		if !u.followAccess(n.Operand) {
			return false
		}
		if key, ok := n.Index.(*syntax.StringNode); ok {
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

func (u *UntrustedInputChecker) OnVisitNodeEnter(n syntax.Node) {
	boolean := false
	switch n.(type) {
	case *syntax.CompareOpNode, *syntax.NotOpNode:
		boolean = true
	}
	if len(u.visits) > 0 {
		switch parent := u.visits[len(u.visits)-1].node.(type) {
		case *syntax.LogicalOpNode:
			boolean = boolean || parent.Kind == syntax.LogicalOpNodeKindAnd && parent.Left == n
		case *syntax.FuncCallNode:
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
	if f, ok := n.(*syntax.FuncCallNode); ok && functions.IsStringPredicate(f.Callee) {
		u.safeCalls++
	}
}

func (u *UntrustedInputChecker) OnVisitNodeLeave(n syntax.Node) {
	defer func() {
		if len(u.visits) > 0 {
			visit := u.visits[len(u.visits)-1]
			u.visits = u.visits[:len(u.visits)-1]
			if visit.boolean {
				u.booleanContexts--
			}
		}
	}()
	if u.safeCalls > 0 {
		if f, ok := n.(*syntax.FuncCallNode); ok && functions.IsStringPredicate(f.Callee) {
			u.safeCalls--
		}
		return
	}

	switch n := n.(type) {
	case *syntax.VariableNode:
		u.end()
		u.onVar(n)
	case *syntax.ObjectDerefNode:
		u.onPropAccess(n.Property)
		if u.start != nil {
			u.start = n
		}
	case *syntax.IndexAccessNode:
		if lit, ok := n.Index.(*syntax.StringNode); ok {
			u.onPropAccess(strings.ToLower(lit.Value))
		} else {
			u.onIndexAccess()
		}
		if u.start != nil {
			u.start = n
		}
	case *syntax.ArrayDerefNode:
		u.onObjectFilter()
		if u.start != nil {
			u.start = n
		}
	case *syntax.FuncCallNode:
		if u.booleanContexts == 0 && strings.EqualFold(n.Callee, "toJSON") && len(n.Args) == 1 {
			switch n.Args[0].(type) {
			case *syntax.VariableNode, *syntax.ObjectDerefNode, *syntax.ArrayDerefNode, *syntax.IndexAccessNode:
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

// OnVisitEnd must run after the whole syntax tree is visited.
func (u *UntrustedInputChecker) OnVisitEnd() {
	u.end()
}

func (u *UntrustedInputChecker) Errs() []*typecheck.Error {
	out := make([]*typecheck.Error, len(u.errs))
	for i, issue := range u.errs {
		copied := *issue
		out[i] = &copied
	}
	return out
}

func (u *UntrustedInputChecker) Init() {
	u.errs = u.errs[:0]
	u.safeCalls = 0
	u.booleanContexts = 0
	u.visits = u.visits[:0]
	u.reset()
}

func scriptInjectionIssuef(code string, node syntax.Node, format string, args ...any) *typecheck.Error {
	span := syntax.ExpressionSpan(node)
	return &typecheck.Error{Rule: "script-injection", Code: code, Message: fmt.Sprintf(format, args...), Offset: span.Start, EndOffset: span.End, Position: span.Position}
}

func checkUntrustedInputs(expr syntax.Node) []*typecheck.Error {
	checker := NewUntrustedInputChecker(BuiltinUntrustedInputs)
	syntax.Visit(expr, func(node, _ syntax.Node, entering bool) {
		if entering {
			checker.OnVisitNodeEnter(node)
		} else {
			checker.OnVisitNodeLeave(node)
		}
	})
	checker.OnVisitEnd()
	return checker.Errs()
}
