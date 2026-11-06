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
	completion := commandstatus.NewCheckResult(code)
	r := completion
	if a.result != nil {
		r = a.result.report
		r.Status, r.Completed, r.ExitCode = completion.Status, completion.Completed, completion.ExitCode
		if a.result.code >= commandstatus.ExitInvalidCommandOption {
			r.Error = a.result.stderr
		}
	}
	if failure != nil {
		r.Error = failure.Error()
	}
	return r
}
