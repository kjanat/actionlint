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

// SourceSnapshot is a read-only handle to the declarations of one admitted
// source revision. Copies share one admission record, which callers cannot
// reach or overwrite. The zero SourceSnapshot is absent: Present reports
// false, Kind reports KindUnknown and every validation fails.
type SourceSnapshot struct{ admission *admission }

// admission binds immutable declarations to their admitted source revision.
// bind fills every field before the registry publishes it; nothing writes it
// afterwards.
type admission struct {
	registry  *SourceSnapshots
	captured  capture.Snapshot
	reference source.Reference
	action    bool
	bound     *workflowdecl.Bound
}

// Present reports whether s came from a registry. A present snapshot can still
// fail verification after its capture is quarantined.
func (s SourceSnapshot) Present() bool { return s.admission != nil }

func (s SourceSnapshot) Reference() source.Reference {
	if s.admission == nil {
		return source.Reference{}
	}
	return s.admission.reference
}

func (s SourceSnapshot) Content() []byte {
	if s.admission == nil {
		return nil
	}
	return s.admission.captured.Bytes()
}

func (s SourceSnapshot) Capture() capture.Snapshot {
	if s.admission == nil {
		return capture.Snapshot{}
	}
	return s.admission.captured
}

// Identity and byte access remain available for diagnostics after quarantine;
// semantic access requires the capture to remain admitted by its owner.
func (s SourceSnapshot) verify() error {
	if s.admission == nil {
		return errors.New("source snapshot is unavailable")
	}
	return s.admission.verify()
}

func (a *admission) verify() error {
	if a.registry == nil || a.registry.store == nil {
		return errors.New("source snapshot is unavailable")
	}
	return a.registry.store.VerifyHandle(a.captured)
}

// SourceSnapshots owns one declaration revision per document in an analysis.
type SourceSnapshots struct {
	store     *capture.Store
	mu        sync.Mutex
	documents map[string]*admission
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
func (s *SourceSnapshots) Lookup(reference source.Reference) (SourceSnapshot, bool) {
	if s == nil {
		return SourceSnapshot{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	admitted := s.documents[reference.Document]
	if admitted == nil || admitted.reference != reference || admitted.verify() != nil {
		return SourceSnapshot{}, false
	}
	return SourceSnapshot{admitted}, true
}

// LookupDocument finds the one canonical revision already admitted for identity.
// Reference-based callers still use Lookup to validate every reference field.
func (s *SourceSnapshots) LookupDocument(identity string) (SourceSnapshot, bool) {
	if s == nil || identity == "" {
		return SourceSnapshot{}, false
	}
	s.mu.Lock()
	admitted, ok := s.documents[identity]
	s.mu.Unlock()
	if !ok || admitted.verify() != nil {
		return SourceSnapshot{}, false
	}
	return SourceSnapshot{admitted}, true
}

// OwnsSnapshot reports whether this registry admitted snapshot. Snapshots do
// not expose their registry: holding a snapshot grants no admission rights.
func (s *SourceSnapshots) OwnsSnapshot(snapshot SourceSnapshot) bool {
	return s != nil && snapshot.admission != nil && snapshot.admission.registry == s
}

func (s *SourceSnapshots) storedLocked(document, revision string, action bool) (*admission, error) {
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
func (s *SourceSnapshots) Document(captured capture.Snapshot, document Document) (SourceSnapshot, error) {
	record := document.Provenance.record
	if record == nil {
		return SourceSnapshot{}, errors.New("source document has no parse provenance")
	}
	if err := s.verify(captured, []byte(record.Text())); err != nil {
		return SourceSnapshot{}, err
	}
	return s.bind(captured, record)
}

func (s *SourceSnapshots) Action(captured capture.Snapshot, metadata *actionmeta.Metadata) (SourceSnapshot, error) {
	if metadata == nil {
		return SourceSnapshot{}, errors.New("action metadata is unavailable")
	}
	text, parsed := metadata.OriginalSource()
	if !parsed {
		return SourceSnapshot{}, errors.New("action source has no parse provenance")
	}
	if err := s.verify(captured, []byte(text)); err != nil {
		return SourceSnapshot{}, err
	}
	revision := captured.Revision()
	s.mu.Lock()
	previous, err := s.storedLocked(revision.Document(), revision.Digest(), true)
	s.mu.Unlock()
	if err != nil {
		return SourceSnapshot{}, err
	}
	if previous != nil {
		return SourceSnapshot{previous}, nil
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

func (s *SourceSnapshots) bind(captured capture.Snapshot, record *workflowdecl.Record) (SourceSnapshot, error) {
	if err := s.verify(captured, []byte(record.Text())); err != nil {
		return SourceSnapshot{}, err
	}
	document, revision := captured.Revision().Document(), captured.Revision().Digest()
	action := record.Action()
	s.mu.Lock()
	previous, err := s.storedLocked(document, revision, action)
	s.mu.Unlock()
	if err != nil {
		return SourceSnapshot{}, err
	}
	if previous != nil {
		return SourceSnapshot{previous}, nil
	}
	bound := record.Bind(document)
	references := bound.SourceIndex()
	admitted := &admission{
		registry:  s,
		reference: references.Document, captured: captured, bound: bound,
		action: action,
	}
	seen := make(map[source.Reference]struct{}, len(references.Steps))
	for _, reference := range references.Steps {
		if _, duplicate := seen[reference]; duplicate {
			return SourceSnapshot{}, fmt.Errorf("source document %q has duplicate step references", document)
		}
		seen[reference] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, err = s.storedLocked(document, revision, action)
	if err != nil {
		return SourceSnapshot{}, err
	}
	if previous != nil {
		return SourceSnapshot{previous}, nil
	}
	if s.documents == nil {
		s.documents = make(map[string]*admission)
	}
	s.documents[document] = admitted
	return SourceSnapshot{admitted}, nil
}

type DocumentKind uint8

const (
	KindUnknown DocumentKind = iota
	KindWorkflow
	KindAction
)

func (s SourceSnapshot) Kind() DocumentKind {
	if s.verify() != nil || s.admission.bound == nil {
		return KindUnknown
	}
	if s.admission.action {
		return KindAction
	}
	return KindWorkflow
}

func (s SourceSnapshot) WorkflowView() (workflowdecl.WorkflowView, bool) {
	if s.Kind() != KindWorkflow {
		return workflowdecl.WorkflowView{}, false
	}
	view := s.admission.bound.Workflow()
	return view, view.Present()
}

func (s SourceSnapshot) ActionView() (workflowdecl.ActionBodyView, bool) {
	if s.Kind() != KindAction {
		return workflowdecl.ActionBodyView{}, false
	}
	view := s.admission.bound.ActionBody()
	return view, view.Present()
}

func (s *SourceSnapshots) Declaration(reference source.Reference) (workflowdecl.StepView, bool) {
	if s == nil {
		return workflowdecl.StepView{}, false
	}
	s.mu.Lock()
	admitted := s.documents[reference.Document]
	s.mu.Unlock()
	if admitted == nil || admitted.bound == nil || admitted.verify() != nil {
		return workflowdecl.StepView{}, false
	}
	return admitted.bound.Step(reference)
}

func (s *SourceSnapshots) Owns(step workflowdecl.StepView) bool {
	if s == nil || !step.Present() {
		return false
	}
	s.mu.Lock()
	admitted := s.documents[step.SourceReference().Document]
	s.mu.Unlock()
	return admitted != nil && admitted.bound != nil && admitted.verify() == nil && admitted.bound.Owns(step)
}

// CanonicalReferences returns detached source identities and positions.
func (s SourceSnapshot) CanonicalReferences() workflowfacts.SourceIndex {
	if s.verify() != nil || s.admission.bound == nil {
		return workflowfacts.SourceIndex{}
	}
	return s.admission.bound.SourceIndex()
}

func (s SourceSnapshot) Declarations() ([]analyzer.Mapping, []analyzer.Dependency) {
	if s.verify() != nil || s.admission.bound == nil {
		return nil, nil
	}
	return s.admission.bound.Declarations()
}

func (s SourceSnapshot) ValidateWorkflow(view workflowdecl.WorkflowView) error {
	if err := s.verify(); err != nil {
		return err
	}
	if s.admission.action || !s.admission.bound.OwnsWorkflow(view) {
		return errors.New("workflow does not belong to source snapshot")
	}
	return nil
}

func (s SourceSnapshot) ValidateJob(view workflowdecl.JobView) error {
	if err := s.verify(); err != nil {
		return err
	}
	if !s.admission.bound.OwnsJob(view) {
		return errors.New("job does not belong to source snapshot")
	}
	return nil
}

func (s SourceSnapshot) ValidateStep(view workflowdecl.StepView) error {
	if err := s.verify(); err != nil {
		return err
	}
	if !s.admission.bound.Owns(view) {
		return errors.New("step does not belong to source snapshot")
	}
	return nil
}
