package documentparse

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sync"

	documentmodel "actionlint.kjanat.dev/v2/document"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/source"
)

// SourceSnapshot binds declaration references to the exact AST that produced them.
type SourceSnapshot struct {
	reference source.Reference
	content   []byte
	facts     *workflowlang.Workflow
	steps     map[*workflowlang.Step]source.Reference
	action    bool
}

func (s *SourceSnapshot) Reference() source.Reference { return s.reference }
func (s *SourceSnapshot) Content() []byte             { return bytes.Clone(s.content) }

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

// Document returns the canonical indexed AST for this source revision.
// Callers must traverse that snapshot, including when supplied a fresh decode.
func (s *SourceSnapshots) Document(identity string, content []byte, document Document) (*SourceSnapshot, error) {
	if document.Workflow != nil && document.Metadata != nil {
		return nil, errors.New("source document contains both workflow and action declarations")
	}
	action := document.Metadata != nil
	switch document.Outline.(type) {
	case documentmodel.Action:
		if document.Workflow != nil {
			return nil, errors.New("action outline contains workflow declarations")
		}
		action = true
	case documentmodel.Workflow:
		if document.Metadata != nil {
			return nil, errors.New("workflow outline contains action declarations")
		}
	case nil:
		if document.Workflow == nil && document.Metadata == nil {
			return nil, errors.New("source document kind is unavailable")
		}
	default:
		return nil, errors.New("unsupported source document outline")
	}
	return s.bind(identity, content, action, document.Facts)
}

func (s *SourceSnapshots) Action(identity string, metadata *actionmeta.Metadata) (*SourceSnapshot, error) {
	if metadata == nil {
		return nil, errors.New("action metadata is unavailable")
	}
	if metadata.Source == nil {
		return nil, errors.New("action source bytes are unavailable")
	}
	return s.bind(identity, metadata.Source, true, func() *workflowlang.Workflow {
		return actionFacts(metadata)
	})
}

func (s *SourceSnapshots) bind(identity string, content []byte, action bool, declarations func() *workflowlang.Workflow) (*SourceSnapshot, error) {
	if identity == "" {
		return nil, errors.New("source document identity is empty")
	}
	document := sourceid.Document(identity)
	revision := sourceid.Digest(content)
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, exists := s.documents[document]; exists {
		if previous.reference.Revision != revision {
			return nil, fmt.Errorf("source document %q has conflicting revisions in one analysis", document)
		}
		if previous.action != action {
			return nil, fmt.Errorf("source document %q has conflicting declaration kinds", document)
		}
		return previous, nil
	}
	owned := bytes.Clone(content)
	facts := declarations()
	references := workflowfacts.Index(document, owned, facts, action)
	snapshot := &SourceSnapshot{
		reference: references.Document, content: owned, facts: facts,
		steps: references.Steps, action: action,
	}
	if s.documents == nil {
		s.documents = make(map[string]*SourceSnapshot)
	}
	s.documents[document] = snapshot
	return snapshot, nil
}
