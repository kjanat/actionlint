package definition

import (
	"fmt"
	"iter"
	"math"
	"sort"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
)

type scriptSourceLine struct {
	Start int
	End   int
}

// ScriptSource maps spans in a decoded run script back to positions in the YAML source.
type ScriptSource struct {
	ContentStart sourcetext.Position
	ScalarEnd    sourcetext.Position
	Value        string
	Lines        []scriptSourceLine
	Spans        []sourcemap.Span
	Literal      bool
	Comments     []ScalarComment
}

// ScalarComment is a YAML comment on a scalar declaration.
// Pos points to '#', in Unicode character columns.
type ScalarComment struct {
	Text     string
	Pos      sourcetext.Position
	Trailing bool
}

func NewScriptSource(value string) *ScriptSource {
	s := &ScriptSource{Value: value}
	start := 0
	for {
		i := strings.IndexByte(value[start:], '\n')
		if i < 0 {
			s.Lines = append(s.Lines, scriptSourceLine{Start: start, End: len(value)})
			return s
		}
		end := start + i
		s.Lines = append(s.Lines, scriptSourceLine{Start: start, End: end})
		start = end + 1
	}
}

func (s *ScriptSource) Position(line, col int) (*sourcetext.Position, bool) {
	return s.lookup(line, col, false)
}

func (s *ScriptSource) EndPos(line, col int) (*sourcetext.Position, bool) {
	return s.lookup(line, col, true)
}

func (s *ScriptSource) lookup(line, col int, endPosition bool) (*sourcetext.Position, bool) {
	if s == nil || line <= 0 || line > len(s.Lines) || col <= 0 {
		return nil, false
	}
	l := s.Lines[line-1]
	rel, ok := sourcetext.ByteOffsetAtColumn(s.Value[l.Start:l.End], col)
	if !ok {
		return nil, false
	}
	position, ok := sourcemap.Locate(s.Value, s.Spans, l.Start+rel, endPosition)
	if !ok {
		return nil, false
	}
	return &position, true
}

type SourceScalar struct {
	HasSource bool
	Evaluated bool
	Kind      codec.NodeKind
	Tag       string
	Value     string
	Pos       sourcetext.Position
	// End is the exclusive physical end of a dependency scalar, when available.
	End sourcetext.Position
}

type String struct {
	Value  string
	Quoted bool
	Pos    *sourcetext.Position
	// SourceScalar records the original YAML node before normalization or folding.
	SourceScalar SourceScalar
}

func (s *String) ContainsExpression() bool {
	return s != nil && syntax.ContainsExpression(s.Value)
}

func (s *String) Text() string {
	if s == nil {
		return ""
	}
	return s.Value
}

func (s *String) Position() (sourcetext.Position, bool) {
	if s == nil || s.Pos == nil {
		return sourcetext.Position{}, false
	}
	return *s.Pos, true
}

func IsExprAssigned(s string) bool {
	if !strings.HasPrefix(s, "${{") || !strings.HasSuffix(s, "}}") {
		return false
	}
	// Delimiters inside single-quoted strings are data. Doubled quotes toggle
	// twice, keeping the scan inside the same string literal.
	inString := false
	for i := 3; i < len(s)-1; i++ {
		if s[i] == '\'' {
			inString = !inString
		} else if !inString {
			if strings.HasPrefix(s[i:], "}}") {
				return i+2 == len(s)
			}
			if strings.HasPrefix(s[i:], "${{") {
				return false
			}
		}
	}
	// Let the expression parser diagnose an unterminated string literal.
	return inString
}

func (s *String) IsExpressionAssigned() bool {
	return s != nil && IsExprAssigned(s.Value)
}

// ConstantValue evaluates an assigned expression when all its required inputs
// are known. It reports false for an absent String, for text that is not one
// assigned expression and for an expression whose value depends on runtime
// contexts.
func (s *String) ConstantValue() (any, bool) {
	if s == nil {
		return nil, false
	}
	expr := syntax.ParseAssignedExpression(s.Value)
	if expr == nil {
		return nil, false
	}
	return evaluate.ConditionConstantValue(expr)
}

type Bool struct {
	Value      bool
	Expression *String
	Pos        *sourcetext.Position
}

func (b *Bool) String() string {
	if b.Expression != nil {
		return b.Expression.Value
	}
	if b.Value {
		return "true"
	}
	return "false"
}

type Int struct {
	Value      int
	Expression *String
	// TruncatedFrom is the source spelling of a fractional literal GitHub
	// truncates toward zero to produce Value. It is empty for whole numbers.
	TruncatedFrom string
	Pos           *sourcetext.Position
}

type Float struct {
	Value      float64
	Expression *String
	Pos        *sourcetext.Position
}

type Event interface {
	EventName() string
}

type WebhookEventFilter struct {
	Name   *String
	Values []*String
}

func (f *WebhookEventFilter) IsEmpty() bool {
	return f == nil || len(f.Values) == 0
}

type WebhookEvent struct {
	Hook           *String
	Types          []*String
	Branches       *WebhookEventFilter
	BranchesIgnore *WebhookEventFilter
	Tags           *WebhookEventFilter
	TagsIgnore     *WebhookEventFilter
	Paths          *WebhookEventFilter
	PathsIgnore    *WebhookEventFilter
	Workflows      []*String
	Pos            *sourcetext.Position
}

func (e *WebhookEvent) EventName() string {
	return e.Hook.Value
}

type ScheduleEntry struct {
	Cron *String
	// Timezone is the optional IANA timezone for the schedule.
	Timezone *String
}

type ScheduledEvent struct {
	Schedules []*ScheduleEntry
	Pos       *sourcetext.Position
}

func (e *ScheduledEvent) EventName() string {
	return "schedule"
}

type WorkflowDispatchEventInputType uint8

const (
	WorkflowDispatchEventInputTypeNone WorkflowDispatchEventInputType = iota
	WorkflowDispatchEventInputTypeString
	WorkflowDispatchEventInputTypeNumber
	WorkflowDispatchEventInputTypeBoolean
	WorkflowDispatchEventInputTypeChoice
	WorkflowDispatchEventInputTypeEnvironment
)

type DispatchInput struct {
	Name        *String
	Description *String
	Required    *Bool
	Default     *String
	Type        WorkflowDispatchEventInputType
	Options     []*String
}

type WorkflowDispatchEvent struct {
	// Inputs maps lowercase input names to their attributes.
	Inputs map[string]*DispatchInput
	Pos    *sourcetext.Position
}

func (e *WorkflowDispatchEvent) EventName() string {
	return "workflow_dispatch"
}

type RepositoryDispatchEvent struct {
	Types []*String
	Pos   *sourcetext.Position
}

func (e *RepositoryDispatchEvent) EventName() string {
	return "repository_dispatch"
}

type WorkflowCallEventInputType uint8

const (
	WorkflowCallEventInputTypeInvalid WorkflowCallEventInputType = iota
	WorkflowCallEventInputTypeBoolean
	WorkflowCallEventInputTypeNumber
	WorkflowCallEventInputTypeString
)

func ParseWorkflowCallEventInputType(spelling string) (WorkflowCallEventInputType, bool) {
	switch spelling {
	case "boolean":
		return WorkflowCallEventInputTypeBoolean, true
	case "number":
		return WorkflowCallEventInputTypeNumber, true
	case "string":
		return WorkflowCallEventInputTypeString, true
	}
	return WorkflowCallEventInputTypeInvalid, false
}

func (t WorkflowCallEventInputType) ExprType() exprtypes.Type {
	switch t {
	case WorkflowCallEventInputTypeBoolean:
		return exprtypes.BoolType()
	case WorkflowCallEventInputTypeNumber:
		return exprtypes.NumberType()
	case WorkflowCallEventInputTypeString:
		return exprtypes.StringType()
	case WorkflowCallEventInputTypeInvalid:
	}
	return exprtypes.AnyType()
}

func (t WorkflowCallEventInputType) String() string { return t.ExprType().String() }

type WorkflowCallEventInput struct {
	Name        *String
	Description *String
	Default     *String
	Required    *Bool
	Type        WorkflowCallEventInputType
	// ID is the lowercase input ID.
	ID string
}

func (i *WorkflowCallEventInput) IsRequired() bool {
	return i.Required != nil && i.Required.Value
}

type WorkflowCallEventSecret struct {
	Name        *String
	Description *String
	Required    *Bool
}

type WorkflowCallEventOutput struct {
	Name        *String
	Description *String
	Value       *String
}

type WorkflowCallEvent struct {
	// Inputs keeps workflow_call inputs in source order.
	Inputs []*WorkflowCallEventInput
	// Secrets maps lowercase names to secret declarations. Nil means 'secrets'
	// was omitted.
	Secrets map[string]*WorkflowCallEventSecret
	// Outputs maps lowercase names to output declarations.
	Outputs map[string]*WorkflowCallEventOutput
	Pos     *sourcetext.Position
}

func (e *WorkflowCallEvent) EventName() string {
	return "workflow_call"
}

type ImageVersionEvent struct {
	Types []*String
	Names []*String
	// Versions lists patterns matching created image versions.
	Versions []*String
	Pos      *sourcetext.Position
}

func (e *ImageVersionEvent) EventName() string {
	return "image_version"
}

type PermissionScope struct {
	Name  *String
	Value *String
}

type Permissions struct {
	All    *String
	Scopes map[string]*PermissionScope
	Pos    *sourcetext.Position
}

type DefaultsRun struct {
	Expression       *String
	Shell            *String
	WorkingDirectory *String
	Pos              *sourcetext.Position
}

type Defaults struct {
	Run *DefaultsRun
	Pos *sourcetext.Position
}

type Concurrency struct {
	Expression       *String
	Group            *String
	CancelInProgress *Bool
	// Queue sets the strategy for pending jobs or workflow runs: "single" (default) or "max".
	Queue *String
	Pos   *sourcetext.Position
}

type Environment struct {
	Expression *String
	Name       *String
	// URL is the URL mapped to 'environment_url' in the deployments API.
	URL *String
	// Deployment controls whether GitHub creates a deployment. False skips
	// automatic deployment but retains environment protection rules and secrets.
	Deployment *Bool
	Pos        *sourcetext.Position
}

type ExecKind uint8

const (
	ExecKindAction ExecKind = iota
	ExecKindRun
	ExecKindWait
	ExecKindCancel
	ExecKindParallel
)

func (k ExecKind) String() string {
	switch k {
	case ExecKindAction:
		return "uses"
	case ExecKindRun:
		return "run"
	case ExecKindWait:
		return "wait"
	case ExecKindCancel:
		return "cancel"
	case ExecKindParallel:
		return "parallel"
	default:
		return "unknown"
	}
}

type Exec interface {
	Kind() ExecKind
}

type ExecRun struct {
	Run *String
	// Source maps positions in Run.Value back to the original YAML source when
	// they can be mapped exactly.
	Source           *ScriptSource
	Shell            *String
	WorkingDirectory *String
	RunPos           *sourcetext.Position
}

func (e *ExecRun) Kind() ExecKind {
	return ExecKindRun
}

type ActionArgument struct {
	Name   *String
	Value  *String
	Source *ScriptSource
}

type ActionInputMapping struct {
	Inputs     map[string]*ActionArgument
	Expression *String
	Insertions []*ActionInputMapping
}

type ExecAction struct {
	// Uses is nil when its value could not be parsed as a nonempty string.
	Uses *String
	// Inputs maps lowercase names to action inputs from 'with'.
	Inputs           map[string]*ActionArgument
	InputsExpression *String
	InputInsertions  []*ActionInputMapping
	Entrypoint       *String
	Args             *String

	EntrypointEvaluated  bool
	ArgsEvaluated        bool
	InputNameExpressions []*String
}

func (e *ExecAction) Kind() ExecKind {
	return ExecKindAction
}

type ExecWait struct {
	Names  []*String
	All    bool
	AllPos *sourcetext.Position
}

func (e *ExecWait) Kind() ExecKind {
	return ExecKindWait
}

type ExecCancel struct {
	Name *String
}

func (e *ExecCancel) Kind() ExecKind {
	return ExecKindCancel
}

type ExecParallel struct {
	Steps []*Step
}

func (e *ExecParallel) Kind() ExecKind {
	return ExecKindParallel
}

// AllSteps yields steps in source order, each parallel group before the steps
// inside it. It skips nil steps.
func AllSteps(steps []*Step) iter.Seq[*Step] {
	return func(yield func(*Step) bool) {
		walkSteps(steps, yield)
	}
}

func walkSteps(steps []*Step, yield func(*Step) bool) bool {
	for _, step := range steps {
		if step == nil {
			continue
		}
		if !yield(step) {
			return false
		}
		if parallel, ok := step.Exec.(*ExecParallel); ok && parallel != nil && !walkSteps(parallel.Steps, yield) {
			return false
		}
	}
	return true
}

type RawYAMLValueKind int

const (
	RawYAMLValueKindObject = RawYAMLValueKind(4)
	RawYAMLValueKindArray  = RawYAMLValueKind(2)
	RawYAMLValueKindString = RawYAMLValueKind(8)
)

type RawYAMLValue interface {
	Kind() RawYAMLValueKind
	Equals(other RawYAMLValue) bool
	Pos() *sourcetext.Position
	String() string
}

type RawYAMLObject struct {
	// Props maps lowercase property names to values.
	Props    map[string]RawYAMLValue
	Position *sourcetext.Position
}

func (o *RawYAMLObject) Kind() RawYAMLValueKind {
	return RawYAMLValueKindObject
}

func (o *RawYAMLObject) Equals(other RawYAMLValue) bool {
	switch other := other.(type) {
	case *RawYAMLObject:
		if len(o.Props) != len(other.Props) {
			return false
		}
		for n, p1 := range o.Props {
			if p2, ok := other.Props[n]; !ok || !rawEquals(p1, p2) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (o *RawYAMLObject) Pos() *sourcetext.Position {
	return copyPos(o.Position)
}

func (o *RawYAMLObject) String() string {
	qs := make([]string, 0, len(o.Props))
	for n, p := range o.Props {
		qs = append(qs, fmt.Sprintf("%q: %s", n, rawString(p)))
	}
	sort.Strings(qs)
	return "{" + strings.Join(qs, ", ") + "}"
}

type RawYAMLArray struct {
	Elems    []RawYAMLValue
	Position *sourcetext.Position
}

func (a *RawYAMLArray) Kind() RawYAMLValueKind {
	return RawYAMLValueKindArray
}

func (a *RawYAMLArray) Equals(other RawYAMLValue) bool {
	switch other := other.(type) {
	case *RawYAMLArray:
		if len(a.Elems) != len(other.Elems) {
			return false
		}
		for i, e1 := range a.Elems {
			if !rawEquals(e1, other.Elems[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (a *RawYAMLArray) Pos() *sourcetext.Position {
	return copyPos(a.Position)
}

func (a *RawYAMLArray) String() string {
	var b strings.Builder
	b.WriteRune('[')

	comma := false
	for _, e := range a.Elems {
		if comma {
			b.WriteString(", ")
		} else {
			comma = true
		}
		b.WriteString(rawString(e))
	}

	b.WriteRune(']')
	return b.String()
}

type RawYAMLString struct {
	Value string
	// A tag rejected by the parser is recorded as "!!str".
	Tag      string
	Position *sourcetext.Position
}

func (s *RawYAMLString) Kind() RawYAMLValueKind {
	return RawYAMLValueKindString
}

func (s *RawYAMLString) Equals(other RawYAMLValue) bool {
	switch other := other.(type) {
	case *RawYAMLString:
		a, b := s.ScalarValue(), other.ScalarValue()
		if a == b {
			return true
		}
		x, xok := a.(float64)
		y, yok := b.(float64)
		return xok && yok && math.IsNaN(x) && math.IsNaN(y)
	default:
		return false
	}
}

func (s *RawYAMLString) ScalarValue() any {
	switch s.Tag {
	case YamlTagBool:
		if codec.IsCoreSchemaBool(s.Value) {
			return strings.EqualFold(s.Value, "true")
		}
	case YamlTagNull:
		if codec.IsCoreSchemaNull(s.Value) {
			return nil
		}
	case YamlTagInt, YamlTagFloat:
		if number, ok := codec.CoreSchemaNumber(s.Value); ok {
			return number
		}
	}
	return s.Value
}

func (s *RawYAMLString) Pos() *sourcetext.Position {
	return copyPos(s.Position)
}

// copyPos detaches a node position so callers cannot move the node through it.
func copyPos(p *sourcetext.Position) *sourcetext.Position {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

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

func (s *RawYAMLString) String() string {
	if _, ok := s.ScalarValue().(string); ok {
		return strconv.Quote(s.Value)
	}
	if s.Value == "" {
		return "null"
	}
	return s.Value
}

type MatrixRow struct {
	Evaluated  bool
	Name       *String
	Values     []RawYAMLValue
	Expression *String
}

type MatrixAssign struct {
	Key   *String
	Value RawYAMLValue
}

type MatrixCombination struct {
	Evaluated  bool
	Assigns    map[string]*MatrixAssign
	Expression *String
}

type MatrixCombinations struct {
	Name         *String
	Evaluated    bool
	Combinations []*MatrixCombination
	Expression   *String
}

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

type Matrix struct {
	Insertions []*Matrix
	// Rows maps lowercase axis names to their values.
	Rows       map[string]*MatrixRow
	Include    *MatrixCombinations
	Exclude    *MatrixCombinations
	Expression *String
	Pos        *sourcetext.Position
}

type Strategy struct {
	Expression  *String
	Matrix      *Matrix
	FailFast    *Bool
	MaxParallel *Int
	Pos         *sourcetext.Position
}

type EnvVar struct {
	Name  *String
	Value *String
}

type Env struct {
	Vars       map[string]*EnvVar
	Insertions []*Env
	Expression *String
}

type Step struct {
	ID   *String
	If   *String
	Name *String
	Exec Exec
	// DeclaredUses records uses even when a conflicting execution key invalidates it.
	DeclaredUses    *String
	Env             *Env
	ContinueOnError *Bool
	TimeoutMinutes  *Float
	Background      *Bool
	Pos             *sourcetext.Position
}

type Credentials struct {
	Username   *String
	Password   *String
	Expression *String
	Pos        *sourcetext.Position
}

type Container struct {
	Expression        *String
	Image             *String
	Credentials       *Credentials
	Env               *Env
	Ports             []*String
	PortsExpression   *String
	Volumes           []*String
	VolumesExpression *String
	Options           *String
	Command           *String
	Entrypoint        *String
	Pos               *sourcetext.Position
}

type Service struct {
	Name      *String
	Container *Container
}

type Services struct {
	// Value maps lowercase service IDs to services.
	Value      map[string]*Service
	Expression *String
	Pos        *sourcetext.Position
}

type Output struct {
	Name  *String
	Value *String
}

type Runner struct {
	Expression *String
	Labels     []*String
	LabelsExpr *String
	Group      *String
}

type WorkflowCallInput struct {
	Name  *String
	Value *String
}

type WorkflowCallSecret struct {
	Name  *String
	Value *String
}

type WorkflowCall struct {
	Uses *String
	// Inputs maps lowercase input names to values from 'with:'.
	Inputs map[string]*WorkflowCallInput
	// Secrets maps lowercase secret names to values from 'secrets:'.
	Secrets map[string]*WorkflowCallSecret
	// InheritSecrets is true when 'secrets: inherit' is specified.
	InheritSecrets bool
}

type Snapshot struct {
	Expression *String
	ImageName  *String
	Version    *String
	If         *String
}

type Job struct {
	ID          *String
	Name        *String
	Needs       []*String
	RunsOn      *Runner
	Permissions *Permissions
	// CacheMode overrides workflow cache access for this job. Nil means inherit.
	CacheMode            *CacheMode
	Environment          *Environment
	Concurrency          *Concurrency
	Outputs              map[string]*Output
	Env                  *Env
	Defaults             *Defaults
	If                   *String
	Steps                []*Step
	TimeoutMinutes       *Float
	CancelTimeoutMinutes *Float
	Strategy             *Strategy
	ContinueOnError      *Bool
	Container            *Container
	Services             *Services
	WorkflowCall         *WorkflowCall
	// DeclaredUses records uses even when step-only keys invalidate the call.
	DeclaredUses *String
	Snapshot     *Snapshot
	Pos          *sourcetext.Position
}

type Workflow struct {
	ExpressionSources map[sourcetext.Position]*ScriptSource
	Name              *String
	Description       *String
	RunName           *String
	On                []Event
	OnPos             *sourcetext.Position
	Permissions       *Permissions
	// CacheMode sets default cache access. Nil leaves the trigger-dependent default in effect.
	CacheMode   *CacheMode
	Env         *Env
	Defaults    *Defaults
	Concurrency *Concurrency
	// Jobs maps lowercase job IDs to jobs.
	Jobs map[string]*Job
	// Pos is the position of the first workflow mapping key. It may be nil for
	// workflows constructed directly by API callers.
	Pos *sourcetext.Position
}

func FindWorkflowCallEvent(w *Workflow) (*WorkflowCallEvent, bool) {
	for _, e := range w.On {
		if e, ok := e.(*WorkflowCallEvent); ok {
			return e, true
		}
	}
	return nil, false
}
