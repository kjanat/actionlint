package syntax

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func (p *Pos) String() string {
	return fmt.Sprintf("line:%d,col:%d", p.Line, p.Col)
}

// IsBefore returns if the position is before the other position. If they are equal, this function returns false.
func (p *Pos) IsBefore(other *Pos) bool {
	if p.Line != other.Line {
		return p.Line < other.Line
	}
	return p.Col < other.Col
}

// copyPos detaches a position so callers cannot modify the node through an accessor result.
func copyPos(p *Pos) *Pos {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

// ContainsExpression returns whether the String contains at least one ${{ }} expression.
func (s *String) ContainsExpression() bool {
	return (&workflowlang.String{Value: s.Value}).ContainsExpression()
}

// IsExpressionAssigned returns whether a single expression is assigned to the String.
func (s *String) IsExpressionAssigned() bool {
	return workflowlang.IsExprAssigned(s.Value)
}

func (b *Bool) String() string {
	if b.Expression != nil {
		return b.Expression.Value
	}
	return strconv.FormatBool(b.Value)
}

// IsEmpty returns true when it has no value. This may mean the filter itself is nil.
func (f *WebhookEventFilter) IsEmpty() bool {
	return f == nil || len(f.Values) == 0
}

// EventName returns name of the Event to trigger this workflow.
func (e *WebhookEvent) EventName() string { return e.Hook.Value }

// EventName returns name of the Event to trigger this workflow.
func (*ScheduledEvent) EventName() string { return "schedule" }

// EventName returns name of the Event to trigger this workflow.
func (*WorkflowDispatchEvent) EventName() string { return "workflow_dispatch" }

// EventName returns name of the Event to trigger this workflow.
func (*RepositoryDispatchEvent) EventName() string { return "repository_dispatch" }

// EventName returns name of the Event to trigger this workflow.
func (*WorkflowCallEvent) EventName() string { return "workflow_call" }

// EventName returns name of the Event to trigger this workflow.
func (*ImageVersionEvent) EventName() string { return "image_version" }

// IsRequired returns if the input is marked as required or not.
func (i *WorkflowCallEventInput) IsRequired() bool {
	return i.Required != nil && i.Required.Value
}

// String returns the workflow spelling of the cache mode.
func (k CacheModeKind) String() string {
	return workflowlang.CacheModeKind(k).String()
}

// Capabilities returns the restore and save access granted by a declared mode.
func (k CacheModeKind) Capabilities() (uint8, bool) {
	return workflowlang.CacheModeKind(k).Capabilities()
}

// Capabilities returns the access granted by the declaration; a nil declaration grants none.
func (m *CacheMode) Capabilities() (uint8, bool) {
	if m == nil {
		return 0, false
	}
	return m.Kind.Capabilities()
}

// Kind returns kind of the step execution.
func (*ExecRun) Kind() ExecKind { return ExecKindRun }

// Kind returns kind of the step execution.
func (*ExecAction) Kind() ExecKind { return ExecKindAction }

// Kind returns kind of the step execution.
func (*ExecWait) Kind() ExecKind { return ExecKindWait }

// Kind returns kind of the step execution.
func (*ExecCancel) Kind() ExecKind { return ExecKindCancel }

// Kind returns kind of the step execution.
func (*ExecParallel) Kind() ExecKind { return ExecKindParallel }

// ContainsExpression returns if the combinations section includes at least one expression node.
func (cs *MatrixCombinations) ContainsExpression() bool {
	if cs.Expression != nil {
		return true
	}
	for _, c := range cs.Combinations {
		if c.Expression != nil {
			return true
		}
	}
	return false
}

// Kind returns kind of raw YAML value.
func (*RawYAMLObject) Kind() RawYAMLValueKind { return RawYAMLValueKindObject }

// Equals returns if the other value is equal to the value.
func (o *RawYAMLObject) Equals(other RawYAMLValue) bool {
	object, ok := other.(*RawYAMLObject)
	if !ok || len(o.Props) != len(object.Props) {
		return false
	}
	for n, p1 := range o.Props {
		if p2, ok := object.Props[n]; !ok || !rawEquals(p1, p2) {
			return false
		}
	}
	return true
}

// Pos returns a copy of the start position of the value in the source file.
func (o *RawYAMLObject) Pos() *Pos { return copyPos(o.Position) }

func (o *RawYAMLObject) String() string {
	qs := make([]string, 0, len(o.Props))
	for n, p := range o.Props {
		qs = append(qs, fmt.Sprintf("%q: %s", n, rawString(p)))
	}
	sort.Strings(qs)
	return "{" + strings.Join(qs, ", ") + "}"
}

// Kind returns kind of raw YAML value.
func (*RawYAMLArray) Kind() RawYAMLValueKind { return RawYAMLValueKindArray }

// Equals returns if the other value is equal to the value.
func (a *RawYAMLArray) Equals(other RawYAMLValue) bool {
	array, ok := other.(*RawYAMLArray)
	if !ok || len(a.Elems) != len(array.Elems) {
		return false
	}
	for i, e := range a.Elems {
		if !rawEquals(e, array.Elems[i]) {
			return false
		}
	}
	return true
}

// Pos returns a copy of the start position of the value in the source file.
func (a *RawYAMLArray) Pos() *Pos { return copyPos(a.Position) }

func (a *RawYAMLArray) String() string {
	elems := make([]string, 0, len(a.Elems))
	for _, e := range a.Elems {
		elems = append(elems, rawString(e))
	}
	return "[" + strings.Join(elems, ", ") + "]"
}

// Kind returns kind of raw YAML value.
func (*RawYAMLString) Kind() RawYAMLValueKind { return RawYAMLValueKindString }

// Equals returns if the other value is equal to the value. Scalars compare by
// their YAML core schema value; NaN equals NaN.
func (s *RawYAMLString) Equals(other RawYAMLValue) bool {
	scalar, ok := other.(*RawYAMLString)
	if !ok {
		return false
	}
	a, b := s.ScalarValue(), scalar.ScalarValue()
	if a == b {
		return true
	}
	x, xok := a.(float64)
	y, yok := b.(float64)
	return xok && yok && math.IsNaN(x) && math.IsNaN(y)
}

// ScalarValue returns the YAML core schema value of the scalar: bool, nil,
// float64 or the original string.
func (s *RawYAMLString) ScalarValue() any {
	return (&workflowlang.RawYAMLString{Value: s.Value, Tag: s.Tag}).ScalarValue()
}

// Pos returns a copy of the start position of the value in the source file.
func (s *RawYAMLString) Pos() *Pos { return copyPos(s.Position) }

func (s *RawYAMLString) String() string {
	return (&workflowlang.RawYAMLString{Value: s.Value, Tag: s.Tag}).String()
}

// rawEquals dispatches over the closed set of raw YAML node types so nested
// comparisons stay within this package's statically known methods.
func rawEquals(a, b RawYAMLValue) bool {
	switch a := a.(type) {
	case *RawYAMLObject:
		return a.Equals(b)
	case *RawYAMLArray:
		return a.Equals(b)
	case *RawYAMLString:
		return a.Equals(b)
	default:
		return false
	}
}

// rawString renders a nested raw YAML node through its concrete method.
func rawString(v RawYAMLValue) string {
	switch v := v.(type) {
	case *RawYAMLObject:
		return v.String()
	case *RawYAMLArray:
		return v.String()
	case *RawYAMLString:
		return v.String()
	default:
		return ""
	}
}
