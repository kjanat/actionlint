package githubaction

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev"
)

// ToolPlan validates Action configuration before the launcher installs tools.
func ToolPlan(env func(string) string, stdout, stderr io.Writer) int {
	needed, err := requiredTools(env)
	if err == nil {
		err = json.NewEncoder(stdout).Encode(struct {
			SchemaVersion int `json:"schema_version"`
			actionlint.ExternalToolRequirements
		}{1, needed})
	}
	if err == nil {
		return actionlint.ExitStatusSuccessNoProblem
	}
	_, _ = fmt.Fprintln(stderr, err)
	if _, ok := errors.AsType[*inputError](err); ok {
		return actionlint.ExitStatusInvalidCommandOption
	}
	if _, ok := errors.AsType[*actionlint.ConfigOverlayError](err); ok {
		return actionlint.ExitStatusInvalidCommandOption
	}
	return actionlint.ExitStatusFailure
}

func requiredTools(env func(string) string) (actionlint.ExternalToolRequirements, error) {
	var none actionlint.ExternalToolRequirements
	in, err := parseInputs(environmentArgs(env))
	if err != nil {
		return none, err
	}
	a := &action{env: env}
	workspace, err := a.workspace()
	if err != nil {
		return none, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return none, err
	}
	defer func() { _ = root.Close() }()
	req, err := a.prepareRequest(in, root, workspace)
	if err != nil {
		return none, err
	}
	session, err := actionlint.NewAnalysisSession(actionlint.AnalysisOptions{
		WorkingDir: req.workingDir, ConfigFile: req.configFile, ConfigOverlays: req.overlays,
		Shellcheck: req.shellcheck, Pyflakes: req.pyflakes, IgnorePatterns: req.ignore,
	})
	if err != nil {
		return none, err
	}
	return session.RequiredTools(req.files)
}
