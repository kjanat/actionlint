package report

import (
	"encoding/json"

	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/results"
	documentmodel "actionlint.kjanat.dev/v2/internal/results/outline"
)

// CheckResult is the versioned JSON document shared by CLI and Action checks.
// ExitCode describes analysis, even when a caller chooses not to fail on findings.
// Consumers should tolerate unknown fields within a supported schema version.
type CheckResult struct {
	IncompleteChecks []IncompleteCheck      `json:"incomplete_checks,omitempty"`
	ActionMetadata   []ActionMetadataResult `json:"action_metadata"`
	SchemaVersion    int                    `json:"schema_version"`
	Status           string                 `json:"status"`
	Completed        bool                   `json:"completed"`
	ExitCode         int                    `json:"exit_code"`
	FileCount        *int                   `json:"file_count"`
	Diagnostics      []results.Diagnostic   `json:"diagnostics"`
	Documents        documentmodel.Outlines `json:"documents"`
	Configs          []ResultConfig         `json:"configurations"`
	Hints            []string               `json:"hints"`
	SARIF            json.RawMessage        `json:"sarif,omitempty"`
	Error            string                 `json:"error,omitempty"`
}

// ActionMetadataResult describes inspected evidence for one distinct requested
// action reference. Unavailable metadata has a reason, never an invented identity.
type ActionMetadataResult = actions.MetadataReport

// ResultConfig identifies the configuration used for a project, including overlays.
type ResultConfig struct {
	File      string                         `json:"file"`
	Project   string                         `json:"project"`
	Overrides []string                       `json:"overrides"`
	Origins   map[string]configreport.Origin `json:"origins,omitempty"`
	Warnings  []configreport.Warning         `json:"warnings,omitempty"`
}

// FromAnalysis projects captured analysis data without deciding command status.
func FromAnalysis(analysis Analysis) CheckResult {
	r := CheckResult{SchemaVersion: 1, Diagnostics: []results.Diagnostic{}, Configs: []ResultConfig{}, Hints: []string{}, ActionMetadata: []ActionMetadataResult{}}
	count := analysis.FileCount
	r.FileCount = &count
	r.Documents = analysis.Documents
	r.IncompleteChecks = append([]IncompleteCheck(nil), analysis.IncompleteChecks...)
	if analysis.Diagnostics != nil {
		r.Diagnostics = analysis.Diagnostics
	}
	for _, coverage := range analysis.ActionMetadata {
		r.ActionMetadata = append(r.ActionMetadata, coverage.Report())
	}
	for _, config := range analysis.Configurations {
		r.AddConfiguration(config)
	}
	return r
}

// AddConfiguration preserves selection provenance without serializing internal config state.
func (r *CheckResult) AddConfiguration(config configreport.Report) {
	r.Configs = append(r.Configs, ResultConfig{config.File, config.Project, config.Overrides, config.Inspection.Origins, config.Inspection.Warnings})
}
