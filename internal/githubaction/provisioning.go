package githubaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/tools/adapters"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/render"
)

// ToolPlan validates Action configuration before the launcher installs tools.
func ToolPlan(env func(string) string, stdout, stderr io.Writer) int {
	needed, err := requiredTools(env)
	if err == nil {
		err = json.NewEncoder(stdout).Encode(struct {
			SchemaVersion int `json:"schema_version"`
			// Released v1 launchers require this compatibility field.
			Pyflakes   bool `json:"pyflakes"`
			Shellcheck bool `json:"shellcheck"`
			Ruff       bool `json:"ruff"`
		}{1, false, needed["shellcheck"], needed["ruff"]})
	}
	if err == nil {
		return render.ExitSuccessNoProblem
	}
	_, _ = fmt.Fprintln(stderr, err)
	if _, ok := errors.AsType[*inputError](err); ok {
		return render.ExitInvalidCommandOption
	}
	if _, ok := errors.AsType[*repoconfig.OverlayError](err); ok {
		return render.ExitInvalidCommandOption
	}
	return render.ExitFailure
}

func requiredTools(env func(string) string) (application.ToolRequirements, error) {
	var none application.ToolRequirements
	in, err := parseInputs(environmentArgs(env))
	if err != nil {
		return none, err
	}
	a := &action{env: env}
	workspace, err := a.workspace()
	if err != nil {
		return none, err
	}
	req, err := a.prepareRequest(in, workspace)
	if err != nil {
		return none, err
	}
	return application.RequiredTools(context.Background(), application.Options{
		WorkingDir: req.workingDir, ConfigFile: req.configFile, ConfigOverlays: req.overlays,
		IgnorePatterns: req.ignore, Tools: adapters.Options{ShellCheck: req.shellcheck, Ruff: req.ruff, RuffOptions: req.ruffOptions}.Registrations(),
	}, req.files)
}
