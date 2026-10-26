package documentparse

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/analyzer"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/documentparse/internal/frozen"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/source"
)

// SourceSnapshot binds declaration references to the exact AST that produced them.
type SourceSnapshot struct {
	registry     *SourceSnapshots
	reference    source.Reference
	content      []byte
	facts        *workflowlang.Workflow
	steps        map[*workflowlang.Step]source.Reference
	declarations map[source.Reference]StepDeclaration
	action       bool
	bound        *frozen.Bound
}

// StepDeclaration is a read-only view of an indexed declaration and its source.
type StepDeclaration struct {
	Source *SourceSnapshot
	Step   *workflowlang.Step
	Job    string
}

func (s *SourceSnapshot) Reference() source.Reference { return s.reference }
func (s *SourceSnapshot) Content() []byte             { return bytes.Clone(s.content) }
func (s *SourceSnapshot) Registry() *SourceSnapshots  { return s.registry }

// Workflow supplies the indexed AST for read-only workflow traversal.
func (s *SourceSnapshot) Workflow() (*workflowlang.Workflow, bool) {
	if s.action || s.facts == nil {
		return nil, false
	}
	return s.facts, true
}

// ActionSteps supplies the indexed declarations for read-only action traversal.
func (s *SourceSnapshot) ActionSteps() ([]*workflowlang.Step, bool) {
	if !s.action || s.facts == nil {
		return nil, false
	}
	return slices.Clone(s.facts.Jobs[""].Steps), true
}

func (s *SourceSnapshot) StepReference(step *workflowlang.Step) (source.Reference, error) {
	ref, exists := s.steps[step]
	if !exists {
		return source.Reference{}, fmt.Errorf("step does not belong to source snapshot %q", s.reference.Document)
	}
	return ref, nil
}

// SourceSnapshots owns one declaration revision per document in an analysis.
type SourceSnapshots struct {
	mu        sync.Mutex
	documents map[string]*SourceSnapshot
}

// Lookup requires the complete document reference, including revision and node.
func (s *SourceSnapshots) Lookup(reference source.Reference) (*SourceSnapshot, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.documents[reference.Document]
	if snapshot == nil || snapshot.reference != reference {
		return nil, false
	}
	return snapshot, true
}

// LookupDocument finds the one canonical revision already admitted for identity.
// Reference-based callers still use Lookup to validate every reference field.
func (s *SourceSnapshots) LookupDocument(identity string) (*SourceSnapshot, bool) {
	if s == nil || identity == "" {
		return nil, false
	}
	s.mu.Lock()
	snapshot, ok := s.documents[sourceid.Document(identity)]
	s.mu.Unlock()
	return snapshot, ok
}

func (s *SourceSnapshots) Step(reference source.Reference) (StepDeclaration, bool) {
	if s == nil {
		return StepDeclaration{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.documents[reference.Document]
	if snapshot == nil {
		return StepDeclaration{}, false
	}
	declaration, exists := snapshot.declarations[reference]
	return declaration, exists
}

func (s *SourceSnapshots) storedLocked(document, revision string, action bool) (*SourceSnapshot, error) {
	previous := s.documents[document]
	if previous == nil {
		return nil, nil
	}
	if previous.reference.Revision != revision {
		return nil, fmt.Errorf("source document %q has conflicting revisions in one analysis", document)
	}
	if previous.action != action {
		return nil, fmt.Errorf("source document %q has conflicting declaration kinds", document)
	}
	return previous, nil
}

// Document returns the canonical indexed AST for this source revision.
// Callers must traverse that snapshot, including when supplied a fresh decode.
func (s *SourceSnapshots) Document(identity string, content []byte, document Document) (*SourceSnapshot, error) {
	if document.record == nil {
		return nil, errors.New("source document has no parse provenance")
	}
	if !bytes.Equal(content, []byte(document.record.Text())) {
		return nil, errors.New("source content disagrees with parsed revision")
	}
	return s.bind(identity, document.record, document.traversal)
}

func (s *SourceSnapshots) Action(identity string, metadata *actionmeta.Metadata) (*SourceSnapshot, error) {
	if metadata == nil {
		return nil, errors.New("action metadata is unavailable")
	}
	text, parsed := metadata.OriginalSource()
	if !parsed {
		return nil, errors.New("action source has no parse provenance")
	}
	if identity == "" {
		return nil, errors.New("source document identity is empty")
	}
	s.mu.Lock()
	previous, err := s.storedLocked(sourceid.Document(identity), sourceid.Digest([]byte(text)), true)
	s.mu.Unlock()
	if err != nil || previous != nil {
		return previous, err
	}
	// Reparse only the private original text. Mutable metadata is a lookup view,
	// never declaration provenance. Parsing and publication happen outside mu.
	// Metadata admission historically includes recovered declarations even when
	// decoding reports an error; document analysis separately controls eligibility.
	recovered, _ := actionmeta.Parse(metadata.Path(), []byte(text))
	traversal := actionFacts(recovered)
	record := frozen.Publish(text, traversal, true, recovered.Runs.Using)
	return s.bind(identity, record, traversal)
}

func (s *SourceSnapshots) bind(identity string, record *frozen.Record, legacy *workflowlang.Workflow) (*SourceSnapshot, error) {
	if identity == "" {
		return nil, errors.New("source document identity is empty")
	}
	document := sourceid.Document(identity)
	content := []byte(record.Text())
	revision := sourceid.Digest(content)
	action := record.Action()
	s.mu.Lock()
	previous, err := s.storedLocked(document, revision, action)
	s.mu.Unlock()
	if err != nil || previous != nil {
		return previous, err
	}
	bound := record.Bind(document)
	references := bound.CursorReferences()
	snapshot := &SourceSnapshot{
		registry:  s,
		reference: references.Document, content: content, facts: legacy, bound: bound,
		steps: references.Steps, action: action,
		declarations: make(map[source.Reference]StepDeclaration, len(references.Steps)),
	}
	for step, reference := range references.Steps {
		if _, duplicate := snapshot.declarations[reference]; duplicate {
			return nil, fmt.Errorf("source document %q has duplicate step references", document)
		}
		snapshot.declarations[reference] = StepDeclaration{Source: snapshot, Step: step, Job: references.StepJobs[step]}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, err = s.storedLocked(document, revision, action)
	if err != nil || previous != nil {
		return previous, err
	}
	if s.documents == nil {
		s.documents = make(map[string]*SourceSnapshot)
	}
	s.documents[document] = snapshot
	return snapshot, nil
}

type DocumentKind uint8

const (
	KindUnknown DocumentKind = iota
	KindWorkflow
	KindAction
)

func (s *SourceSnapshot) Kind() DocumentKind {
	if s == nil || s.bound == nil {
		return KindUnknown
	}
	if s.action {
		return KindAction
	}
	return KindWorkflow
}

func (s *SourceSnapshot) WorkflowView() (WorkflowView, bool) {
	if s.Kind() != KindWorkflow {
		return WorkflowView{}, false
	}
	view := s.bound.Workflow()
	return view, view.Present()
}

func (s *SourceSnapshot) ActionView() (ActionBodyView, bool) {
	if s.Kind() != KindAction {
		return ActionBodyView{}, false
	}
	view := s.bound.ActionBody()
	return view, view.Present()
}

func (s *SourceSnapshot) StepView(cursor *workflowlang.Step) (StepView, error) {
	if s == nil || s.bound == nil {
		return StepView{}, errors.New("source snapshot is unavailable")
	}
	view, ok := s.bound.StepCursor(cursor)
	if !ok {
		return StepView{}, fmt.Errorf("step does not belong to source snapshot %q", s.reference.Document)
	}
	return view, nil
}

func (s *SourceSnapshot) JobView(cursor *workflowlang.Job) (JobView, error) {
	if s == nil || s.bound == nil {
		return JobView{}, errors.New("source snapshot is unavailable")
	}
	view, ok := s.bound.JobCursor(cursor)
	if !ok {
		return JobView{}, fmt.Errorf("job does not belong to source snapshot %q", s.reference.Document)
	}
	return view, nil
}

func (s *SourceSnapshots) Declaration(reference source.Reference) (StepView, bool) {
	if s == nil {
		return StepView{}, false
	}
	s.mu.Lock()
	snapshot := s.documents[reference.Document]
	s.mu.Unlock()
	if snapshot == nil || snapshot.bound == nil {
		return StepView{}, false
	}
	return snapshot.bound.Step(reference)
}

func (s *SourceSnapshots) Owns(step StepView) bool {
	if s == nil || !step.Present() {
		return false
	}
	s.mu.Lock()
	snapshot := s.documents[step.SourceReference().Document]
	s.mu.Unlock()
	return snapshot != nil && snapshot.bound != nil && snapshot.bound.Owns(step)
}

// CanonicalReferences returns detached index maps keyed by legacy traversal
// cursors. References and positions are retained from the owned parse record.
func (s *SourceSnapshot) CanonicalReferences() workflowfacts.References {
	if s == nil || s.bound == nil {
		return workflowfacts.References{}
	}
	return s.bound.CursorReferences()
}

func (s *SourceSnapshot) Declarations() ([]analyzer.Mapping, []analyzer.Dependency) {
	if s == nil || s.bound == nil {
		return nil, nil
	}
	return s.bound.Declarations()
}
