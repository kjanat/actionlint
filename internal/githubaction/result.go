package githubaction

import (
	"encoding/json"
	"os"

	"actionlint.kjanat.dev"
)

// Internal aliases keep reporters on the public result contract.
type persistedResult = actionlint.CheckResult

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

func (a *action) checkResult(code int, failure error) actionlint.CheckResult {
	if a.result != nil && failure == nil {
		code = a.result.code
	}
	r := actionlint.NewCheckResult(code)
	if a.result != nil {
		lint := a.result
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
		if lint.code >= actionlint.ExitStatusInvalidCommandOption {
			r.Error = lint.stderr
		}
	}
	if failure != nil {
		r.Error = failure.Error()
	}
	return r
}
