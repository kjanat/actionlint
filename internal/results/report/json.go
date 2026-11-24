package report

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/outline"
)

// CheckResult consumers should tolerate unknown fields within a supported schema version.
type CheckResult struct {
	IncompleteChecks []IncompleteCheck        `json:"incomplete_checks,omitempty"`
	ActionMetadata   []actions.MetadataReport `json:"action_metadata"`
	SchemaVersion    int                      `json:"schema_version"`
	Status           string                   `json:"status"`
	Completed        bool                     `json:"completed"`
	ExitCode         int                      `json:"exit_code"`
	FailureThreshold severity.Severity        `json:"failure_threshold,omitempty"`
	FileCount        *int                     `json:"file_count"`
	Diagnostics      []results.Diagnostic     `json:"diagnostics"`
	Documents        outline.Outlines         `json:"documents"`
	Configs          []ResultConfig           `json:"configurations"`
	Hints            []string                 `json:"hints"`
	SARIF            json.RawMessage          `json:"sarif,omitempty"`
	Error            string                   `json:"error,omitempty"`
}

type ResultConfig struct {
	File      string                         `json:"file"`
	Project   string                         `json:"project"`
	Overrides []string                       `json:"overrides"`
	Origins   map[string]configreport.Origin `json:"origins,omitempty"`
	Warnings  []configreport.Warning         `json:"warnings,omitempty"`
}

func FromAnalysis(analysis Analysis) CheckResult {
	r := CheckResult{SchemaVersion: 1, Diagnostics: []results.Diagnostic{}, Configs: []ResultConfig{}, Hints: []string{}, ActionMetadata: []actions.MetadataReport{}}
	count := analysis.FileCount
	r.FileCount = &count
	r.Documents = analysis.Documents
	r.IncompleteChecks = append([]IncompleteCheck(nil), analysis.IncompleteChecks...)
	if analysis.Diagnostics != nil {
		r.Diagnostics = results.Diagnostics(analysis.Diagnostics)
	}
	for _, coverage := range analysis.ActionMetadata {
		r.ActionMetadata = append(r.ActionMetadata, coverage.Report())
	}
	for _, config := range analysis.Configurations {
		r.AddConfiguration(config)
	}
	return r
}

func (r *CheckResult) AddConfiguration(config configreport.Report) {
	r.Configs = append(r.Configs, ResultConfig{config.File, config.Project, config.Overrides, config.Inspection.Origins, config.Inspection.Warnings})
}
