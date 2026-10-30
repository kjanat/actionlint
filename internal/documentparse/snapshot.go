package documentparse

import (
	"errors"
	"fmt"
	"sync"

	"actionlint.kjanat.dev/v2/analyzer"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/source"
)

// SourceSnapshot binds immutable declarations to their admitted source revision.
type SourceSnapshot struct {
	registry  *SourceSnapshots
	captured  capture.Snapshot
	reference source.Reference
	action    bool
	bound     *workflowdecl.Bound
}

func (s *SourceSnapshot) Reference() source.Reference { return s.reference }
func (s *SourceSnapshot) Content() []byte             { return s.captured.Bytes() }
func (s *SourceSnapshot) Capture() capture.Snapshot   { return s.captured }

// Identity and byte access remain available for diagnostics after quarantine;
// semantic access requires the capture to remain admitted by its owner.
func (s *SourceSnapshot) verify() error {
	if s == nil || s.registry == nil || s.registry.store == nil {
		return errors.New("source snapshot is unavailable")
	}
	return s.registry.store.VerifyHandle(s.captured)
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

// OwnsSnapshot reports whether this registry admitted snapshot. Snapshots do
// not expose their registry: holding a snapshot grants no admission rights.
func (s *SourceSnapshots) OwnsSnapshot(snapshot *SourceSnapshot) bool {
	return s != nil && snapshot != nil && snapshot.registry == s
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

// Document returns canonical declarations for this source revision.
// Repeated decodes share the existing immutable binding.
func (s *SourceSnapshots) Document(captured capture.Snapshot, document Document) (*SourceSnapshot, error) {
	record := document.Provenance.record
	if record == nil {
		return nil, errors.New("source document has no parse provenance")
	}
	if err := s.verify(captured, []byte(record.Text())); err != nil {
		return nil, err
	}
	return s.bind(captured, record)
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
	record := workflowdecl.Publish(text, traversal, true, recovered.Runs.Using)
	return s.bind(captured, record)
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

func (s *SourceSnapshots) bind(captured capture.Snapshot, record *workflowdecl.Record) (*SourceSnapshot, error) {
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
	references := bound.SourceIndex()
	snapshot := &SourceSnapshot{
		registry:  s,
		reference: references.Document, captured: captured, bound: bound,
		action: action,
	}
	seen := make(map[source.Reference]struct{}, len(references.Steps))
	for _, reference := range references.Steps {
		if _, duplicate := seen[reference]; duplicate {
			return nil, fmt.Errorf("source document %q has duplicate step references", document)
		}
		seen[reference] = struct{}{}
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

func (s *SourceSnapshot) WorkflowView() (workflowdecl.WorkflowView, bool) {
	if s.Kind() != KindWorkflow {
		return workflowdecl.WorkflowView{}, false
	}
	view := s.bound.Workflow()
	return view, view.Present()
}

func (s *SourceSnapshot) ActionView() (workflowdecl.ActionBodyView, bool) {
	if s.Kind() != KindAction {
		return workflowdecl.ActionBodyView{}, false
	}
	view := s.bound.ActionBody()
	return view, view.Present()
}

func (s *SourceSnapshots) Declaration(reference source.Reference) (workflowdecl.StepView, bool) {
	if s == nil {
		return workflowdecl.StepView{}, false
	}
	s.mu.Lock()
	snapshot := s.documents[reference.Document]
	s.mu.Unlock()
	if snapshot == nil || snapshot.bound == nil || snapshot.verify() != nil {
		return workflowdecl.StepView{}, false
	}
	return snapshot.bound.Step(reference)
}

func (s *SourceSnapshots) Owns(step workflowdecl.StepView) bool {
	if s == nil || !step.Present() {
		return false
	}
	s.mu.Lock()
	snapshot := s.documents[step.SourceReference().Document]
	s.mu.Unlock()
	return snapshot != nil && snapshot.bound != nil && snapshot.verify() == nil && snapshot.bound.Owns(step)
}

// CanonicalReferences returns detached source identities and positions.
func (s *SourceSnapshot) CanonicalReferences() workflowfacts.SourceIndex {
	if s.verify() != nil || s.bound == nil {
		return workflowfacts.SourceIndex{}
	}
	return s.bound.SourceIndex()
}

func (s *SourceSnapshot) Declarations() ([]analyzer.Mapping, []analyzer.Dependency) {
	if s.verify() != nil || s.bound == nil {
		return nil, nil
	}
	return s.bound.Declarations()
}

func (s *SourceSnapshot) ValidateWorkflow(view workflowdecl.WorkflowView) error {
	if err := s.verify(); err != nil {
		return err
	}
	if s.action || !s.bound.OwnsWorkflow(view) {
		return errors.New("workflow does not belong to source snapshot")
	}
	return nil
}
func (s *SourceSnapshot) ValidateJob(view workflowdecl.JobView) error {
	if err := s.verify(); err != nil {
		return err
	}
	if !s.bound.OwnsJob(view) {
		return errors.New("job does not belong to source snapshot")
	}
	return nil
}
func (s *SourceSnapshot) ValidateStep(view workflowdecl.StepView) error {
	if err := s.verify(); err != nil {
		return err
	}
	if !s.bound.Owns(view) {
		return errors.New("step does not belong to source snapshot")
	}
	return nil
}
