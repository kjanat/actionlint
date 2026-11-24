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

// SourceSnapshot withholds its declarations once the capture store quarantines its bytes.
// Every validation fails on the zero SourceSnapshot.
type SourceSnapshot struct{ admission *admission }

// bind fills every admission field before the registry publishes it, and nothing writes it afterwards.
type admission struct {
	registry *SourceSnapshots
	captured capture.CapturedDocument
	action   bool
	bound    *workflows.BoundDocument
}

// Present stays true after the capture is quarantined.
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

// SourceSnapshots cannot admit documents when it is the zero value.
type SourceSnapshots struct {
	store     *capture.Store
	mu        sync.Mutex
	documents map[string]*admission
}

func NewSourceSnapshots(store *capture.Store) (*SourceSnapshots, error) {
	if store == nil {
		return nil, errors.New("source declarations require a capture store")
	}
	return &SourceSnapshots{store: store}, nil
}

// Capture neither canonicalizes path nor reads files.
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

func (s *SourceSnapshots) Admit(captured capture.CapturedDocument, parsed *ParsedSource) (SourceSnapshot, error) {
	if parsed == nil {
		return SourceSnapshot{}, errors.New("source document was not parsed")
	}
	if err := s.verify(captured, []byte(parsed.text)); err != nil {
		return SourceSnapshot{}, err
	}
	return s.bind(captured, parsed)
}

func (s *SourceSnapshots) Action(captured capture.CapturedDocument, metadata *actionmeta.Metadata) (SourceSnapshot, error) {
	if metadata == nil {
		return SourceSnapshot{}, errors.New("action metadata is unavailable")
	}
	text, parsed := metadata.Text()
	if !parsed {
		return SourceSnapshot{}, errors.New("action metadata was not parsed")
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
	// Reparse only the private original text, outside mu.
	recovered, parseErr := actionmeta.ParseActionMetadata(metadata.Path(), []byte(text))
	var traversal *definition.Workflow
	if _, rejected := errors.AsType[*codec.AdmissionError](parseErr); !rejected {
		traversal = actionFacts(recovered, text)
	}
	return s.bind(captured, &ParsedSource{text: text, workflow: traversal, action: true, using: recovered.Runs.Using})
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

func (s *SourceSnapshots) bind(captured capture.CapturedDocument, parsed *ParsedSource) (SourceSnapshot, error) {
	if err := s.verify(captured, []byte(parsed.text)); err != nil {
		return SourceSnapshot{}, err
	}
	document, revision := captured.Revision().Document(), captured.Revision().Digest()
	action := parsed.action
	s.mu.Lock()
	previous, err := s.storedLocked(document, revision, action)
	s.mu.Unlock()
	if err != nil {
		return SourceSnapshot{}, err
	}
	if previous != nil {
		return SourceSnapshot{previous}, nil
	}
	bound := parsed.bind(document)
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

type DocumentKind uint8

const (
	KindUnknown DocumentKind = iota
	KindWorkflow
	KindAction
)

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

// BoundDocument returns nil once the capture is quarantined.
func (s SourceSnapshot) BoundDocument() *workflows.BoundDocument {
	if s.verify() != nil {
		return nil
	}
	return s.admission.bound
}

// ValidateWorkflow returns the quarantine cause after quarantine and rejects views this snapshot did not bind.
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
