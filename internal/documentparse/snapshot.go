package documentparse

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/analyzer"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/documentparse/internal/frozen"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/source"
)

// SourceSnapshot binds declaration references to the exact AST that produced them.
type SourceSnapshot struct {
	registry     *SourceSnapshots
	captured     capture.Snapshot
	reference    source.Reference
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
func (s *SourceSnapshot) Content() []byte             { return s.captured.Bytes() }
func (s *SourceSnapshot) Capture() capture.Snapshot   { return s.captured }
func (s *SourceSnapshot) Registry() *SourceSnapshots  { return s.registry }

// Identity and byte access remain available for diagnostics after quarantine;
// semantic access requires the capture to remain admitted by its owner.
func (s *SourceSnapshot) verify() error {
	if s == nil || s.registry == nil || s.registry.store == nil {
		return errors.New("source snapshot is unavailable")
	}
	return s.registry.store.VerifyHandle(s.captured)
}

// Workflow supplies the indexed AST for read-only workflow traversal.
func (s *SourceSnapshot) Workflow() (*workflowlang.Workflow, bool) {
	if s.verify() != nil || s.action || s.facts == nil {
		return nil, false
	}
	return s.facts, true
}

// ActionSteps supplies the indexed declarations for read-only action traversal.
func (s *SourceSnapshot) ActionSteps() ([]*workflowlang.Step, bool) {
	if s.verify() != nil || !s.action || s.facts == nil {
		return nil, false
	}
	return slices.Clone(s.facts.Jobs[""].Steps), true
}

func (s *SourceSnapshot) StepReference(step *workflowlang.Step) (source.Reference, error) {
	if err := s.verify(); err != nil {
		return source.Reference{}, err
	}
	ref, exists := s.steps[step]
	if !exists {
		return source.Reference{}, fmt.Errorf("step does not belong to source snapshot %q", s.reference.Document)
	}
	return ref, nil
}

// SourceSnapshots owns one declaration revision per document in an analysis.
type SourceSnapshots struct {
	store     *capture.Store
	mu        sync.Mutex
	documents map[string]*SourceSnapshot
}

// NewSourceSnapshots binds the declaration registry to one result's byte owner.
// The zero registry cannot admit documents.
func NewSourceSnapshots(store *capture.Store) (*SourceSnapshots, error) {
	if store == nil {
		return nil, errors.New("source declarations require a capture store")
	}
	return &SourceSnapshots{store: store}, nil
}

// Capture looks up already-captured bytes without canonicalizing or reading.
func (s *SourceSnapshots) Capture(path string) (capture.Snapshot, error) {
	if s == nil || s.store == nil {
		return capture.Snapshot{}, errors.New("source declarations require a capture store")
	}
	captured, state, err := s.store.Lookup(path)
	if err != nil {
		return capture.Snapshot{}, err
	}
	if state != capture.Captured {
		return capture.Snapshot{}, fmt.Errorf("source %q has no captured revision", path)
	}
	return captured, nil
}

// Lookup requires the complete document reference, including revision and node.
func (s *SourceSnapshots) Lookup(reference source.Reference) (*SourceSnapshot, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.documents[reference.Document]
	if snapshot == nil || snapshot.reference != reference || snapshot.verify() != nil {
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
	snapshot, ok := s.documents[identity]
	s.mu.Unlock()
	if !ok || snapshot.verify() != nil {
		return nil, false
	}
	return snapshot, true
}

func (s *SourceSnapshots) Step(reference source.Reference) (StepDeclaration, bool) {
	if s == nil {
		return StepDeclaration{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.documents[reference.Document]
	if snapshot == nil || snapshot.verify() != nil {
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
		return nil, s.store.Quarantine(previous.captured, "conflicting declaration revisions")
	}
	if previous.action != action {
		return nil, s.store.Quarantine(previous.captured, "conflicting declaration kinds: action, workflow")
	}
	if err := previous.verify(); err != nil {
		return nil, err
	}
	return previous, nil
}

// Document returns the canonical indexed AST for this source revision.
// Callers must traverse that snapshot, including when supplied a fresh decode.
func (s *SourceSnapshots) Document(captured capture.Snapshot, document Document) (*SourceSnapshot, error) {
	if document.record == nil {
		return nil, errors.New("source document has no parse provenance")
	}
	if err := s.verify(captured, []byte(document.record.Text())); err != nil {
		return nil, err
	}
	return s.bind(captured, document.record, document.traversal)
}

func (s *SourceSnapshots) Action(captured capture.Snapshot, metadata *actionmeta.Metadata) (*SourceSnapshot, error) {
	if metadata == nil {
		return nil, errors.New("action metadata is unavailable")
	}
	text, parsed := metadata.OriginalSource()
	if !parsed {
		return nil, errors.New("action source has no parse provenance")
	}
	if err := s.verify(captured, []byte(text)); err != nil {
		return nil, err
	}
	revision := captured.Revision()
	s.mu.Lock()
	previous, err := s.storedLocked(revision.Document(), revision.Digest(), true)
	s.mu.Unlock()
	if err != nil || previous != nil {
		return previous, err
	}
	// Reparse only the private original text. Mutable metadata is a lookup view,
	// never declaration provenance. Parsing and publication happen outside mu.
	// Schema errors preserve recovered declarations; resource rejection prevents expansion.
	recovered, parseErr := actionmeta.Parse(metadata.Path(), []byte(text))
	var traversal *workflowlang.Workflow
	if _, rejected := errors.AsType[*codec.AdmissionError](parseErr); !rejected {
		traversal = actionFacts(recovered)
	}
	record := frozen.Publish(text, traversal, true, recovered.Runs.Using)
	return s.bind(captured, record, traversal)
}

func (s *SourceSnapshots) verify(captured capture.Snapshot, content []byte) error {
	if s == nil || s.store == nil {
		return errors.New("source declarations require a capture store")
	}
	if err := s.store.Verify(captured, content); err != nil {
		return err
	}
	if captured.Coordinates() != source.YAML {
		return errors.New("source declarations require YAML coordinates")
	}
	return nil
}

func (s *SourceSnapshots) bind(captured capture.Snapshot, record *frozen.Record, legacy *workflowlang.Workflow) (*SourceSnapshot, error) {
	if err := s.verify(captured, []byte(record.Text())); err != nil {
		return nil, err
	}
	document, revision := captured.Revision().Document(), captured.Revision().Digest()
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
		reference: references.Document, captured: captured, facts: legacy, bound: bound,
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
	if s.verify() != nil || s.bound == nil {
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
	if err := s.verify(); err != nil {
		return StepView{}, err
	}
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
	if err := s.verify(); err != nil {
		return JobView{}, err
	}
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
	if snapshot == nil || snapshot.bound == nil || snapshot.verify() != nil {
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
	return snapshot != nil && snapshot.bound != nil && snapshot.verify() == nil && snapshot.bound.Owns(step)
}

// CanonicalReferences returns detached index maps keyed by legacy traversal
// cursors. References and positions come from the owned parse record.
func (s *SourceSnapshot) CanonicalReferences() workflowfacts.References {
	if s.verify() != nil || s.bound == nil {
		return workflowfacts.References{}
	}
	return s.bound.CursorReferences()
}

func (s *SourceSnapshot) Declarations() ([]analyzer.Mapping, []analyzer.Dependency) {
	if s.verify() != nil || s.bound == nil {
		return nil, nil
	}
	return s.bound.Declarations()
}
