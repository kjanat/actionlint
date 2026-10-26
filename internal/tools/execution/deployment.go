package execution

import (
	"fmt"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/source"
)

// Deployment binds an action's display path and runtime placement to its source.
// Its zero value belongs to a workflow entry.
type Deployment struct {
	reference  source.Reference
	sourcePath string
	actionPath string
}

func NewDeployment(snapshot *documentparse.SourceSnapshot, displayPath string, mapped bool) (Deployment, error) {
	if snapshot == nil || displayPath == "" {
		return Deployment{}, fmt.Errorf("%w: action deployment needs a source and path", ErrInvalidBatch)
	}
	if _, action := snapshot.ActionSteps(); !action || snapshot.Reference().Document != sourceid.Document(fsread.PhysicalPath(displayPath)) {
		return Deployment{}, fmt.Errorf("%w: action deployment path disagrees with source", ErrInvalidBatch)
	}
	d := Deployment{reference: snapshot.Reference(), sourcePath: displayPath}
	if !mapped {
		d.actionPath = filepath.Dir(displayPath)
	}
	return d, nil
}

func (d Deployment) SourcePath() string { return d.sourcePath }
func (d Deployment) ActionPath() string { return d.actionPath }
