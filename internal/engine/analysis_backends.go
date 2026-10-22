package engine

import "errors"

func (request AnalysisRequest) validateBackends() error {
	if request.RuffBackend != nil && (request.RuffOptions != nil || request.Ruff != "") {
		return errors.New("ruff selects both a backend and a process")
	}
	if request.ShellcheckBackend != nil && (request.ShellcheckOptions != nil || request.ShellCheck != "" || request.ShellcheckRunner != nil || request.ShellcheckSettings != nil) {
		return errors.New("shellcheck selects both a backend and process settings")
	}
	return nil
}
