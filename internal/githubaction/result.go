package githubaction

import (
	"encoding/json"
	"os"

	"actionlint.kjanat.dev"
)

// persistedResult is the transport between the native analysis and Action reporters.
// Its exit code records analysis status before fail-on-error changes the step status.
type persistedResult struct {
	SchemaVersion int                     `json:"schema_version"`
	Status        string                  `json:"status"`
	Completed     bool                    `json:"completed"`
	ExitCode      int                     `json:"exit_code"`
	FileCount     *int                    `json:"file_count"`
	Diagnostics   []actionlint.Diagnostic `json:"diagnostics"`
	Configs       []resultConfig          `json:"configurations"`
	Hints         []string                `json:"hints"`
	SARIF         json.RawMessage         `json:"sarif,omitempty"`
	Error         string                  `json:"error,omitempty"`
}

type resultConfig struct {
	File      string                             `json:"file"`
	Project   string                             `json:"project"`
	Overrides []string                           `json:"overrides"`
	Origins   map[string]actionlint.ConfigOrigin `json:"origins,omitempty"`
	Warnings  []actionlint.ConfigWarning         `json:"warnings,omitempty"`
}

func (a *action) persistResult(code int, failure error) error {
	path := a.env("ACTIONLINT_ACTION_RESULT")
	if path == "" {
		return nil
	}
	r := persistedResult{SchemaVersion: 1, ExitCode: code, Diagnostics: []actionlint.Diagnostic{},
		Configs: []resultConfig{}, Hints: []string{}}
	if a.result != nil {
		lint := a.result
		if failure == nil {
			r.ExitCode = lint.code
		}
		if lint.fileCountKnown {
			r.FileCount = &lint.fileCount
		}
		if lint.diagnostics != nil {
			r.Diagnostics = lint.diagnostics
		}
		for _, config := range lint.configs {
			r.Configs = append(r.Configs, resultConfig{config.File, config.Project, config.Overrides, config.Inspection.Origins, config.Inspection.Warnings})
		}
		if lint.hints != nil {
			r.Hints = lint.hints
		}
		if lint.sarif != "" {
			r.SARIF = json.RawMessage(lint.sarif)
		}
		if lint.code >= actionlint.ExitStatusInvalidCommandOption {
			r.Error = lint.stderr
		}
	}
	if failure != nil {
		r.Error = failure.Error()
	}
	r.Status = results[r.ExitCode]
	if r.Status == "" {
		r.Status = "failure"
		r.ExitCode = actionlint.ExitStatusFailure
	}
	r.Completed = r.ExitCode == actionlint.ExitStatusSuccessNoProblem || r.ExitCode == actionlint.ExitStatusSuccessProblemFound
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
