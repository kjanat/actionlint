package scripts

import (
	"fmt"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
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
func NewDeployment(snapshots *parsing.SourceSnapshots, snapshot parsing.SourceSnapshot, address, displayPath string, mapped bool) (Deployment, error) {
	if !snapshot.Present() || address == "" || displayPath == "" {
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
	if snapshot.Kind() != parsing.KindAction || captured.Revision().Document() != ref.Document || captured.Revision().Digest() != ref.Revision {
		return Deployment{}, fmt.Errorf("%w: action deployment path disagrees with source", ErrInvalidBatch)
	}
	d := Deployment{reference: ref, address: address, sourcePath: displayPath}
	if !mapped && !source.RemoteAddress(address) {
		d.actionPath = filepath.Dir(displayPath)
	}
	return d, nil
}

func (d Deployment) SourcePath() string { return d.sourcePath }
func (d Deployment) Address() string    { return d.address }
func (d Deployment) ActionPath() string { return d.actionPath }
