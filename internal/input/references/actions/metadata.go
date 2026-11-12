package actions

import (
	"slices"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

// MetadataState distinguishes usable, unavailable and invalid manifest evidence.
type MetadataState string

const (
	MetadataAvailable   MetadataState = "available"
	MetadataUnavailable MetadataState = "unavailable"
	MetadataInvalid     MetadataState = "invalid"
)

// ManifestProblem locates a problem in the selected manifest.
// Selection identifies the bytes that were inspected.
type ManifestProblem struct {
	Code    string             `json:"code,omitempty"`
	Message string             `json:"message"`
	Path    string             `json:"path"`
	Start   sourcepos.Position `json:"start"`
	End     sourcepos.Position `json:"end"`
}

// MetadataAvailability retains why metadata-dependent checks could or could not
// run. Its zero value is unavailable/not-fetched; unavailable is never invalid.
type MetadataAvailability struct {
	state     MetadataState
	selection Selection
	filename  string
	missing   availability.Unavailability
	problems  []ManifestProblem
}

func AvailableMetadata(manifest Manifest) MetadataAvailability {
	if !manifest.Available() {
		return MissingMetadata(availability.NotFetched())
	}
	return MetadataAvailability{state: MetadataAvailable, selection: manifest.Selection(), filename: manifest.Filename()}
}

func InvalidMetadata(manifest Manifest, problems []ManifestProblem) MetadataAvailability {
	if !manifest.Available() || len(problems) == 0 {
		return MissingMetadata(availability.Failed())
	}
	return MetadataAvailability{state: MetadataInvalid, selection: manifest.Selection(), filename: manifest.Filename(), problems: slices.Clone(problems)}
}

func MissingMetadata(reason availability.Unavailability) MetadataAvailability {
	return MetadataAvailability{missing: reason}
}

func (m MetadataAvailability) State() MetadataState {
	if m.state == "" {
		return MetadataUnavailable
	}
	return m.state
}

func (m MetadataAvailability) Reason() availability.Reason {
	if m.State() != MetadataUnavailable {
		return ""
	}
	return m.missing.Reason()
}
func (m MetadataAvailability) Selection() Selection        { return m.selection }
func (m MetadataAvailability) Problems() []ManifestProblem { return slices.Clone(m.problems) }
func (m MetadataAvailability) ManifestPath() string {
	if m.filename == "" {
		return ""
	}
	if directory := m.selection.Identity().Directory(); directory != "" {
		return directory + "/" + m.filename
	}
	return m.filename
}

// Coverage records the outcome for one distinct remote reference requested by
// metadata-dependent analysis. It does not claim execution or transitive coverage.
type Coverage struct {
	Reference    string
	Availability MetadataAvailability
}

// MetadataReport is the detached JSON/API projection of one coverage outcome.
// When metadata is unavailable, Reason explains why and selection fields are empty.
type MetadataReport struct {
	Reference      string              `json:"reference"`
	State          MetadataState       `json:"state"`
	Reason         availability.Reason `json:"reason,omitempty"`
	Host           string              `json:"host,omitempty"`
	Repository     string              `json:"repository,omitempty"`
	RepositoryID   repositoryid.ID     `json:"repository_id,omitzero"`
	Commit         string              `json:"commit,omitempty"`
	Directory      string              `json:"directory,omitempty"`
	ObservedAt     string              `json:"observed_at,omitempty"`
	Manifest       string              `json:"manifest,omitempty"`
	SelectedObject string              `json:"selected_object,omitempty"`
	SelectedRef    string              `json:"selected_ref,omitempty"`
	Problems       []ManifestProblem   `json:"problems,omitempty"`
}

func (c Coverage) Report() MetadataReport {
	a := c.Availability
	s := a.Selection()
	i := s.Identity()
	return MetadataReport{Reference: c.Reference, State: a.State(), Reason: a.Reason(), Host: i.Host(), Repository: i.Repository(), RepositoryID: i.RepositoryID(), Commit: i.Commit(), Directory: i.Directory(), ObservedAt: s.ObservedAt(), Manifest: a.ManifestPath(), SelectedObject: s.Object(), SelectedRef: s.Route().Reference(), Problems: a.Problems()}
}
