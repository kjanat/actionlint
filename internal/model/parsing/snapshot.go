package parsing

import (
	"errors"
	"fmt"
	"sync"

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// SourceSnapshot is a read-only handle to the declarations of one admitted
// source revision. Over the workflows.BoundDocument it holds, it adds the captured
// bytes, the document kind and the owning registry, and it withholds the
// declarations once the capture store quarantines those bytes. Copies share
// one admission record, which callers cannot reach or overwrite. The zero
// SourceSnapshot is absent: Present reports false, Kind reports KindUnknown,
// BoundDocument returns nil and every validation fails.
type SourceSnapshot struct{ admission *admission }

// admission binds immutable declarations to their admitted source revision.
// bind fills every field before the registry publishes it; nothing writes it
// afterwards.
type admission struct {
	registry *SourceSnapshots
	captured capture.CapturedDocument
	action   bool
	bound    *workflows.BoundDocument
}

// Present reports whether s came from a registry. A present snapshot can still
// fail verification after its capture is quarantined.
func (s SourceSnapshot) Present() bool { return s.admission != nil }

func (s SourceSnapshot) Reference() sourcetext.Reference {
	if s.admission == nil {
		return sourcetext.Reference{}
	}
	return s.admission.bound.Reference()
}

func (s SourceSnapshot) Content() []byte {
	if s.admission == nil {
		return nil
	}
	return s.admission.captured.Bytes()
}

func (s SourceSnapshot) Capture() capture.CapturedDocument {
	if s.admission == nil {
		return capture.CapturedDocument{}
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
func (s *SourceSnapshots) Capture(path string) (capture.CapturedDocument, error) {
	if s == nil || s.store == nil {
		return capture.CapturedDocument{}, errors.New("source declarations require a capture store")
	}
	captured, state, err := s.store.Lookup(path)
	if err != nil {
		return capture.CapturedDocument{}, err
	}
	if state != capture.Captured {
		return capture.CapturedDocument{}, fmt.Errorf("source %q has no captured revision", path)
	}
	return captured, nil
}

// Lookup requires the complete document reference, including revision and node.
func (s *SourceSnapshots) Lookup(reference sourcetext.Reference) (SourceSnapshot, bool) {
	if s == nil {
		return SourceSnapshot{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	admitted := s.documents[reference.Document]
	if admitted == nil || admitted.bound.Reference() != reference || admitted.verify() != nil {
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
	if previous.bound.Reference().Revision != revision {
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
func (s *SourceSnapshots) Document(captured capture.CapturedDocument, document Document) (SourceSnapshot, error) {
	provenance := document.Provenance
	if !provenance.parsed {
		return SourceSnapshot{}, errors.New("source document has no parse provenance")
	}
	if err := s.verify(captured, []byte(provenance.text)); err != nil {
		return SourceSnapshot{}, err
	}
	return s.bind(captured, provenance)
}

func (s *SourceSnapshots) Action(captured capture.CapturedDocument, metadata *actionmeta.Metadata) (SourceSnapshot, error) {
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
	recovered, parseErr := actionmeta.ParseActionMetadata(metadata.Path(), []byte(text))
	var traversal *definition.Workflow
	if _, rejected := errors.AsType[*codec.AdmissionError](parseErr); !rejected {
		traversal = actionFacts(recovered)
	}
	return s.bind(captured, Provenance{parsed: true, text: text, workflow: traversal, action: true, using: recovered.Runs.Using})
}

func (s *SourceSnapshots) verify(captured capture.CapturedDocument, content []byte) error {
	if s == nil || s.store == nil {
		return errors.New("source declarations require a capture store")
	}
	if err := s.store.Verify(captured, content); err != nil {
		return err
	}
	if captured.Coordinates() != sourcetext.YAML {
		return errors.New("source declarations require YAML coordinates")
	}
	return nil
}

func (s *SourceSnapshots) bind(captured capture.CapturedDocument, provenance Provenance) (SourceSnapshot, error) {
	if err := s.verify(captured, []byte(provenance.text)); err != nil {
		return SourceSnapshot{}, err
	}
	document, revision := captured.Revision().Document(), captured.Revision().Digest()
	action := provenance.action
	s.mu.Lock()
	previous, err := s.storedLocked(document, revision, action)
	s.mu.Unlock()
	if err != nil {
		return SourceSnapshot{}, err
	}
	if previous != nil {
		return SourceSnapshot{previous}, nil
	}
	bound := provenance.bind(document)
	references := bound.SourceIndex()
	admitted := &admission{registry: s, captured: captured, bound: bound, action: action}
	seen := make(map[sourcetext.Reference]struct{}, len(references.Steps))
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

// DocumentKind identifies the document language: a workflow or an action manifest.
type DocumentKind uint8

const (
	KindUnknown DocumentKind = iota
	KindWorkflow
	KindAction
)

// String returns the language name given to analyzers: "workflow", "action",
// or empty for KindUnknown.
func (k DocumentKind) String() string {
	switch k {
	case KindWorkflow:
		return "workflow"
	case KindAction:
		return "action"
	default:
		return ""
	}
}

func (s SourceSnapshot) Kind() DocumentKind {
	if s.verify() != nil {
		return KindUnknown
	}
	if s.admission.action {
		return KindAction
	}
	return KindWorkflow
}

func (s SourceSnapshot) WorkflowView() (workflows.BoundWorkflow, bool) {
	if s.Kind() != KindWorkflow {
		return workflows.BoundWorkflow{}, false
	}
	view := s.admission.bound.Workflow()
	return view, view.Present()
}

func (s SourceSnapshot) ActionView() (workflows.ActionBody, bool) {
	if s.Kind() != KindAction {
		return workflows.ActionBody{}, false
	}
	view := s.admission.bound.ActionBody()
	return view, view.Present()
}

func (s *SourceSnapshots) Declaration(reference sourcetext.Reference) (workflows.BoundStep, bool) {
	if s == nil {
		return workflows.BoundStep{}, false
	}
	s.mu.Lock()
	admitted := s.documents[reference.Document]
	s.mu.Unlock()
	if admitted == nil || admitted.verify() != nil {
		return workflows.BoundStep{}, false
	}
	return admitted.bound.Step(reference)
}

func (s *SourceSnapshots) Owns(step workflows.BoundStep) bool {
	if s == nil || !step.Present() {
		return false
	}
	s.mu.Lock()
	admitted := s.documents[step.SourceReference().Document]
	s.mu.Unlock()
	return admitted != nil && admitted.verify() == nil && admitted.bound.Owns(step)
}

// BoundDocument returns the snapshot's declarations while its capture remains
// admitted, and nil otherwise. BoundDocument's SourceIndex and Declarations accept a
// nil receiver, so callers read either through this method directly.
func (s SourceSnapshot) BoundDocument() *workflows.BoundDocument {
	if s.verify() != nil {
		return nil
	}
	return s.admission.bound
}

// ValidateWorkflow checks, on every call, that the capture remains admitted and
// that view was bound by this snapshot, returning the quarantine cause when the
// capture is no longer admitted. It also rejects the workflow view of an action
// body. ValidateJob and ValidateStep apply the same checks to jobs and steps.
func (s SourceSnapshot) ValidateWorkflow(view workflows.BoundWorkflow) error {
	if err := s.verify(); err != nil {
		return err
	}
	if s.admission.action || !s.admission.bound.OwnsWorkflow(view) {
		return errors.New("workflow does not belong to source snapshot")
	}
	return nil
}

func (s SourceSnapshot) ValidateJob(view workflows.BoundJob) error {
	if err := s.verify(); err != nil {
		return err
	}
	if !s.admission.bound.OwnsJob(view) {
		return errors.New("job does not belong to source snapshot")
	}
	return nil
}

func (s SourceSnapshot) ValidateStep(view workflows.BoundStep) error {
	if err := s.verify(); err != nil {
		return err
	}
	if !s.admission.bound.Owns(view) {
		return errors.New("step does not belong to source snapshot")
	}
	return nil
}
