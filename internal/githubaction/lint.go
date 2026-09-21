package githubaction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"actionlint.kjanat.dev"
)

const lintTimeout = 300 * time.Second

type lintRequest struct {
	ctx                context.Context
	shellcheckOptions  *actionlint.ExternalCommandOptions
	shellcheckSettings *actionlint.ShellcheckSettings
	pyflakesOptions    *actionlint.ExternalCommandOptions
	workingDir         string
	configFile         string
	overlays           []actionlint.ConfigOverlay
	ignore             []string
	shellcheck         string
	pyflakes           string
	format             string
	files              []string
}

type lintOutcome struct {
	stdout string
	stderr string
	code   int
}

type lintResult struct {
	*lintOutcome
	fileCount      int
	fileCountKnown bool
	configs        []actionlint.ConfigReport
	hints          []string
}

func (req *lintRequest) configureEnvironment(env func(string) string) error {
	for _, name := range append([]string{"config"}, actionlint.ConfigKeys()...) {
		value := env("INPUT_" + strings.ToUpper(name))
		if strings.TrimSpace(value) == "" {
			continue
		}
		overlay, err := actionlint.ParseConfigOverlay(name, []byte(value))
		if err != nil {
			return inputErrorf("%s", err)
		}
		req.overlays = append(req.overlays, overlay)
	}
	if command := env("ACTIONLINT_SHELLCHECK_COMMAND"); req.shellcheck != "" && command != "" {
		req.shellcheckOptions = &actionlint.ExternalCommandOptions{Executable: &command}
	}
	if command := env("ACTIONLINT_PYFLAKES_COMMAND"); req.pyflakes != "" && command != "" {
		req.pyflakesOptions = &actionlint.ExternalCommandOptions{Executable: &command}
	}
	python, script := env("ACTIONLINT_PYTHON"), env("ACTIONLINT_PYFLAKES_SCRIPT")
	if req.pyflakes != "" && python != "" && script != "" {
		req.pyflakesOptions = &actionlint.ExternalCommandOptions{Executable: &python, Arguments: []string{"-I", script}}
	}
	return nil
}

func buildRequest(in *inputs, workspaceDir, workingRel string) (*lintRequest, error) {
	req := &lintRequest{
		workingDir: filepath.Join(workspaceDir, workingRel),
		ignore:     in.ignore,
		format:     "{{json .}}",
	}
	if in.format == formatSARIF {
		req.format = actionlint.SARIFTemplate()
	}
	if in.configFile != "" {
		rel, err := workspaceRel(workspaceDir, filepath.Join(workingRel, in.configFile), "config-file")
		if err != nil {
			return nil, err
		}
		req.configFile = filepath.Join(workspaceDir, rel)
	}
	if in.shellcheck {
		req.shellcheck = "shellcheck"
	}
	if in.pyflakes {
		req.pyflakes = "pyflakes"
	}
	for _, f := range in.files {
		if _, err := workspaceRel(workspaceDir, filepath.Join(workingRel, f), "files"); err != nil {
			return nil, err
		}
	}
	req.files = in.files
	return req, nil
}

func runLinter(req *lintRequest) *lintResult {
	var out, logs bytes.Buffer
	result := &lintResult{}
	opts := actionlint.AnalysisOptions{
		Context:            req.ctx,
		ShellcheckOptions:  req.shellcheckOptions,
		ShellcheckSettings: req.shellcheckSettings,
		PyflakesOptions:    req.pyflakesOptions,
		Shellcheck:         req.shellcheck,
		Pyflakes:           req.pyflakes,
		IgnorePatterns:     req.ignore,
		ConfigFile:         req.configFile,
		ConfigOverlays:     req.overlays,
		WorkingDir:         req.workingDir,
		LogWriter:          &logs,
		OnConfigLoaded: func(report actionlint.ConfigReport) {
			result.configs = append(result.configs, report)
		},
		OnFilesSelected: func(files []string) {
			result.fileCount = len(files)
			result.fileCountKnown = true
		},
	}

	session, err := actionlint.NewAnalysisSession(opts)
	if err != nil {
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}

	renderer, err := actionlint.NewAnalysisRenderer("", req.format, false)
	if err != nil {
		result.lintOutcome = &lintOutcome{"", err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}
	var analysis *actionlint.AnalysisResult
	if len(req.files) == 0 {
		analysis, err = session.Repository(req.workingDir)
	} else {
		analysis, err = session.Files(req.files, nil)
	}
	if err != nil {
		code := actionlint.ExitStatusFailure
		if _, ok := errors.AsType[*actionlint.ConfigOverlayError](err); ok {
			code = actionlint.ExitStatusInvalidCommandOption
		}
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", code}
		return result
	}
	if err := renderer.Render(&out, analysis); err != nil {
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}
	session.Completed(analysis)
	if len(analysis.Diagnostics) > 0 {
		result.hints = quotedIgnoreHints(req.ignore, analysis.Diagnostics)
		result.lintOutcome = &lintOutcome{out.String(), logs.String(), actionlint.ExitStatusSuccessProblemFound}
		return result
	}
	result.lintOutcome = &lintOutcome{out.String(), logs.String(), actionlint.ExitStatusSuccessNoProblem}
	return result
}

func chdir(dir string) (func(), error) {
	prev, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(dir); err != nil {
		return nil, err
	}
	return func() { _ = os.Chdir(prev) }, nil
}

func (a *action) runLint(req *lintRequest) *lintResult {
	restore, err := chdir(req.workingDir)
	if err != nil {
		return &lintResult{lintOutcome: &lintOutcome{"", err.Error() + "\n", actionlint.ExitStatusFailure}}
	}
	defer restore()

	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	req.ctx = ctx
	result := a.lint(req)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		msg := fmt.Sprintf("actionlint timed out after %d seconds\n", int(a.timeout.Seconds()))
		return &lintResult{lintOutcome: &lintOutcome{"", msg, actionlint.ExitStatusFailure}}
	}
	return result
}
