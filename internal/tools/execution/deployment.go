package execution

import (
	"fmt"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/source"
)

// Deployment binds an action's display path and runtime placement to its source.
// Its zero value belongs to a workflow entry.
type Deployment struct {
	reference  source.Reference
	address    string
	sourcePath string
	actionPath string
}

// NewDeployment requires snapshot to be admitted by snapshots and address to
// resolve to its captured revision.
func NewDeployment(snapshots *documentparse.SourceSnapshots, snapshot *documentparse.SourceSnapshot, address, displayPath string, mapped bool) (Deployment, error) {
	if snapshot == nil || address == "" || displayPath == "" {
		return Deployment{}, fmt.Errorf("%w: action deployment needs a source and path", ErrInvalidBatch)
	}
	if !snapshots.OwnsSnapshot(snapshot) {
		return Deployment{}, fmt.Errorf("%w: action deployment source belongs to a different registry", ErrInvalidBatch)
	}
	captured, err := snapshots.Capture(address)
	if err != nil {
		return Deployment{}, fmt.Errorf("%w: action deployment address: %w", ErrInvalidBatch, err)
	}
	ref := snapshot.Reference()
	if snapshot.Kind() != documentparse.KindAction || captured.Revision().Document() != ref.Document || captured.Revision().Digest() != ref.Revision {
		return Deployment{}, fmt.Errorf("%w: action deployment path disagrees with source", ErrInvalidBatch)
	}
	d := Deployment{reference: ref, address: address, sourcePath: displayPath}
	if !mapped {
		d.actionPath = filepath.Dir(displayPath)
	}
	return d, nil
}

func (d Deployment) SourcePath() string { return d.sourcePath }
func (d Deployment) Address() string    { return d.address }
func (d Deployment) ActionPath() string { return d.actionPath }
