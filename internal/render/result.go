package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	documentmodel "actionlint.kjanat.dev/v2/document"

	"actionlint.kjanat.dev/v2/diagnostic"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/report"
)

// Result is a completed analysis prepared for rendering. Findings carry the
// sources text output needs; Diagnostics is the complete machine-readable list.
type Result struct {
	FileCount      int
	Diagnostics    []diagnostic.Diagnostic
	Findings       []Finding
	Documents      documentmodel.Outlines
	Configurations []repoconfig.Report
	Rules          []Rule
}

// Prepare adapts an analysis snapshot for display, converting half-open spans
// to inclusive columns.
func Prepare(analysis report.Analysis) Result {
	r := Result{
		FileCount: analysis.FileCount, Diagnostics: analysis.Diagnostics,
		Documents: analysis.Documents, Configurations: analysis.Configurations, Rules: analysis.Rules,
	}
	for i, d := range analysis.Diagnostics {
		var source report.DiagnosticSource
		if i < len(analysis.Sources) {
			source = analysis.Sources[i]
		}
		finding := FindingOf(d, source.Content)
		finding.RawScript = source.RawScript
		r.Findings = append(r.Findings, finding)
	}
	return r
}

// CheckResult is the versioned JSON document shared by CLI and Action checks.
// ExitCode describes analysis, even when a caller chooses not to fail on findings.
// Consumers should tolerate unknown fields within a supported schema version.
type CheckResult struct {
	SchemaVersion int                     `json:"schema_version"`
	Status        string                  `json:"status"`
	Completed     bool                    `json:"completed"`
	ExitCode      int                     `json:"exit_code"`
	FileCount     *int                    `json:"file_count"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"`
	Documents     documentmodel.Outlines  `json:"documents"`
	Configs       []ResultConfig          `json:"configurations"`
	Hints         []string                `json:"hints"`
	SARIF         json.RawMessage         `json:"sarif,omitempty"`
	Error         string                  `json:"error,omitempty"`
}

// ResultConfig identifies the configuration used for a project, including overlays.
type ResultConfig struct {
	File      string                       `json:"file"`
	Project   string                       `json:"project"`
	Overrides []string                     `json:"overrides"`
	Origins   map[string]repoconfig.Origin `json:"origins,omitempty"`
	Warnings  []repoconfig.Warning         `json:"warnings,omitempty"`
}

// CheckSummary counts the selected inputs and reported findings.
type CheckSummary struct {
	Files    int `json:"files"`
	Findings int `json:"findings"`
}

// NewCheckResult initializes a result with a consistent status and non-null lists.
func NewCheckResult(code int) CheckResult {
	r := CheckResult{SchemaVersion: 1, ExitCode: code, Diagnostics: []diagnostic.Diagnostic{}, Configs: []ResultConfig{}, Hints: []string{}}
	switch code {
	case ExitSuccessNoProblem:
		r.Status, r.Completed = "success", true
	case ExitSuccessProblemFound:
		r.Status, r.Completed = "problems-found", true
	case ExitInvalidCommandOption:
		r.Status = "invalid-options"
	default:
		r.Status, r.ExitCode = "failure", ExitFailure
	}
	return r
}

// AddConfiguration preserves selection provenance without serializing internal config state.
func (r *CheckResult) AddConfiguration(config repoconfig.Report) {
	r.Configs = append(r.Configs, ResultConfig{config.File, config.Project, config.Overrides, config.Inspection.Origins, config.Inspection.Warnings})
}

// WriteJSON writes the complete result, or one versioned diagnostic per line.
// JSONL is a findings stream; completion and failure status belong to the result document.
func (r *CheckResult) WriteJSON(out io.Writer, lines bool) error {
	enc := json.NewEncoder(out)
	if !lines {
		return enc.Encode(r)
	}
	for _, d := range r.Diagnostics {
		if err := enc.Encode(struct {
			SchemaVersion int `json:"schema_version"`
			diagnostic.Diagnostic
		}{r.SchemaVersion, d}); err != nil {
			return err
		}
	}
	return nil
}

// CheckResult exports the result using the shared public result contract.
func (r Result) CheckResult() CheckResult {
	code := ExitSuccessNoProblem
	if len(r.Diagnostics) > 0 {
		code = ExitSuccessProblemFound
	}
	result := NewCheckResult(code)
	count := r.FileCount
	result.FileCount = &count
	result.Documents = r.Documents
	if r.Diagnostics != nil {
		result.Diagnostics = r.Diagnostics
	}
	for _, config := range r.Configurations {
		result.AddConfiguration(config)
	}
	return result
}

// WriteGitHub writes one workflow command annotation per diagnostic.
func WriteGitHub(out io.Writer, diagnostics []diagnostic.Diagnostic) error {
	data := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	property := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	for _, diagnostic := range diagnostics {
		endLine := diagnostic.End.Line
		if endLine > diagnostic.Start.Line && diagnostic.End.Column == 1 {
			endLine--
		}
		position := fmt.Sprintf("line=%d,endLine=%d", diagnostic.Start.Line, endLine)
		if diagnostic.Start.Line == diagnostic.End.Line {
			position += fmt.Sprintf(",col=%d,endColumn=%d", diagnostic.Start.Column, max(diagnostic.Start.Column, diagnostic.End.Column-1))
		}
		level := "error"
		switch diagnostic.Severity {
		case "warning":
			level = "warning"
		case "info", "style", "note":
			level = "notice"
		}
		if _, err := fmt.Fprintf(out, "::%s file=%s,%s,title=%s::%s\n", level, property.Replace(diagnostic.Path), position, property.Replace(diagnostic.Rule), data.Replace(diagnostic.Message)); err != nil {
			return err
		}
	}
	return nil
}
