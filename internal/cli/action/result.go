package githubaction

import (
	"encoding/json"
	"os"

	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

// persistedResult is the JSON document written for the Action wrapper.
type persistedResult = resultreport.CheckResult

func (a *action) persistResult(code int, failure error) error {
	path := a.env("ACTIONLINT_ACTION_RESULT")
	if path == "" {
		return nil
	}
	data, err := json.Marshal(a.checkResult(code, failure))
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (a *action) checkResult(code int, failure error) resultreport.CheckResult {
	if a.result != nil && failure == nil {
		code = a.result.code
	}
	r := commandstatus.NewCheckResult(code)
	if a.result != nil {
		lint := a.result
		r.Documents = lint.documents
		if lint.actionMetadata != nil {
			r.ActionMetadata = lint.actionMetadata
		}
		if lint.fileCountKnown {
			r.FileCount = &lint.fileCount
		}
		if lint.diagnostics != nil {
			r.Diagnostics = lint.diagnostics
		}
		for _, config := range lint.configs {
			r.AddConfiguration(config)
		}
		if lint.hints != nil {
			r.Hints = lint.hints
		}
		if lint.sarif != "" {
			r.SARIF = json.RawMessage(lint.sarif)
		}
		if lint.code >= commandstatus.ExitInvalidCommandOption {
			r.Error = lint.stderr
		}
	}
	if failure != nil {
		r.Error = failure.Error()
	}
	return r
}
