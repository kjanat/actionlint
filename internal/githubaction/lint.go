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
	format             outputFormat
	files              []string
}

type lintOutcome struct {
	stdout string
	stderr string
	code   int
}

type lintResult struct {
	*lintOutcome
	diagnostics    []actionlint.Diagnostic
	sarif          string
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
		format:     in.format,
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
		ShellcheckOptions:  toolOptionsInDirectory(req.shellcheckOptions, req.workingDir),
		ShellcheckSettings: req.shellcheckSettings,
		PyflakesOptions:    toolOptionsInDirectory(req.pyflakesOptions, req.workingDir),
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

	format, template := actionlint.OutputFormat(""), "{{json .}}"
	if req.format == formatSARIF {
		format, template = actionlint.OutputFormatSARIF, ""
	}
	renderer, err := actionlint.NewAnalysisRenderer(format, template, false)
	if err != nil {
		result.lintOutcome = &lintOutcome{"", err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}
	var analysis *actionlint.AnalysisResult
	inputNames := make(map[string]string, len(req.files))
	if len(req.files) == 0 {
		analysis, err = session.Repository(req.workingDir)
	} else {
		paths := make([]string, len(req.files))
		for i, path := range req.files {
			if !filepath.IsAbs(path) {
				inputNames[filepath.Clean(path)] = path
				path = filepath.Join(req.workingDir, path)
			}
			paths[i] = path
		}
		analysis, err = session.Files(paths, nil)
	}
	if err != nil {
		code := actionlint.ExitStatusFailure
		if _, ok := errors.AsType[*actionlint.ConfigOverlayError](err); ok {
			code = actionlint.ExitStatusInvalidCommandOption
		}
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", code}
		return result
	}
	// Absolute reads must retain the caller's relative spelling in Action output.
	for i := range analysis.Diagnostics {
		if path, ok := inputNames[analysis.Diagnostics[i].Path]; ok {
			analysis.Diagnostics[i].Path = path
		}
	}
	if err := renderer.Render(&out, analysis); err != nil {
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}
	result.diagnostics = analysis.Diagnostics
	var sarif bytes.Buffer
	sarifRenderer, err := actionlint.NewAnalysisRenderer(actionlint.OutputFormatSARIF, "", false)
	if err == nil {
		err = sarifRenderer.Render(&sarif, analysis)
	}
	if err != nil {
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}
	result.sarif = sarif.String()
	session.Completed(analysis)
	if len(analysis.Diagnostics) > 0 {
		result.hints = quotedIgnoreHints(req.ignore, analysis.Diagnostics)
		result.lintOutcome = &lintOutcome{out.String(), logs.String(), actionlint.ExitStatusSuccessProblemFound}
		return result
	}
	result.lintOutcome = &lintOutcome{out.String(), logs.String(), actionlint.ExitStatusSuccessNoProblem}
	return result
}

func toolOptionsInDirectory(options *actionlint.ExternalCommandOptions, directory string) *actionlint.ExternalCommandOptions {
	configured := actionlint.ExternalCommandOptions{}
	if options != nil {
		configured = *options
	}
	configured.WorkingDir = directory
	return &configured
}

func (a *action) runLint(req *lintRequest) *lintResult {
	info, err := os.Stat(req.workingDir)
	if err != nil {
		return &lintResult{lintOutcome: &lintOutcome{"", err.Error() + "\n", actionlint.ExitStatusFailure}}
	}
	if !info.IsDir() {
		msg := fmt.Sprintf("working directory %q is not a directory\n", req.workingDir)
		return &lintResult{lintOutcome: &lintOutcome{"", msg, actionlint.ExitStatusFailure}}
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	request := *req
	request.ctx = ctx
	completed := make(chan *lintResult, 1)
	go func() {
		completed <- a.lint(&request)
	}()
	select {
	case result := <-completed:
		if ctx.Err() == nil {
			return result
		}
	case <-ctx.Done():
	}
	// The analysis owns its buffers until it returns, even if it ignores cancellation.
	msg := fmt.Sprintf("actionlint timed out after %d seconds\n", int(a.timeout.Seconds()))
	return &lintResult{lintOutcome: &lintOutcome{"", msg, actionlint.ExitStatusFailure}}
}
