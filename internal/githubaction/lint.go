package githubaction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"actionlint.kjanat.dev"
)

const lintTimeout = 300 * time.Second

// Allow canceled analysis to drain callbacks and hand back its partial result.
const lintCancellationGrace = time.Second

type lintRequest struct {
	ctx                context.Context
	shellcheckOptions  *actionlint.ExternalCommandOptions
	shellcheckSettings *actionlint.ShellcheckSettings
	pyflakesOptions    *actionlint.ExternalCommandOptions
	workingDir         string
	workspaceDir       string
	configFile         string
	overlays           []actionlint.ConfigOverlay
	ignore             []string
	shellcheck         string
	pyflakes           string
	format             outputFormat
	sarif              bool
	files              []string
	hints              []string
}

type lintOutcome struct {
	stdout string
	stderr string
	code   int
}

type lintResult struct {
	*lintOutcome
	diagnostics    []actionlint.Diagnostic
	workflows      []actionlint.WorkflowOutline
	sarif          string
	fileCount      int
	fileCountKnown bool
	configs        []actionlint.ConfigReport
	hints          []string
}

func (req *lintRequest) configureEnvironment(env func(string) string) error {
	req.sarif = env("INPUT_SARIF") == "true"
	if value := env("INPUT_CONFIG"); strings.TrimSpace(value) != "" {
		overlay, err := actionlint.ParseConfigOverlay("config", []byte(value))
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

func buildRequest(in *inputs, workspaceDir, workingDir string) *lintRequest {
	req := &lintRequest{
		workingDir:   inputPath(workspaceDir, workingDir),
		workspaceDir: workspaceDir,
		ignore:       in.ignore,
		format:       in.format,
	}
	if in.configFile != "" {
		req.configFile = inputPath(req.workingDir, in.configFile)
	}
	if in.shellcheck {
		req.shellcheck = "shellcheck"
	}
	if in.pyflakes {
		req.pyflakes = "pyflakes"
	}
	req.files = in.files
	return req
}

func runLinter(req *lintRequest) *lintResult {
	var out, logs bytes.Buffer
	result := &lintResult{hints: slices.Clone(req.hints)}
	workspace := req.workspaceDir
	if workspace == "" {
		workspace = req.workingDir
	}
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
	// Absolute reads must retain the caller's relative spelling in Action output.
	if analysis != nil {
		for i := range analysis.Workflows {
			if path, ok := inputNames[analysis.Workflows[i].Path]; ok {
				analysis.Workflows[i].Path = path
			}
		}
		result.workflows = analysis.Workflows
		for i := range analysis.Diagnostics {
			diagnostic := &analysis.Diagnostics[i]
			if path, ok := inputNames[diagnostic.Path]; ok {
				for j := range diagnostic.Fixes {
					for k := range diagnostic.Fixes[j].Edits {
						edit := &diagnostic.Fixes[j].Edits[k]
						if filepath.Clean(edit.Path) == filepath.Clean(diagnostic.Path) {
							edit.Path = path
						}
					}
				}
				diagnostic.Path = path
			}
		}
		result.diagnostics = analysis.Diagnostics
	}
	if err != nil {
		code := actionlint.ExitStatusFailure
		if _, ok := errors.AsType[*actionlint.ConfigOverlayError](err); ok {
			code = actionlint.ExitStatusInvalidCommandOption
		}
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", code}
		return result
	}
	if req.format == formatSARIF || req.sarif {
		var sarif bytes.Buffer
		sarifRenderer, err := actionlint.NewAnalysisRenderer(actionlint.OutputFormatSARIF, "", false)
		if err == nil {
			err = sarifRenderer.Render(&sarif, workspaceReportAnalysis(analysis, req.workingDir, workspace))
		}
		if err != nil {
			result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
			return result
		}
		result.sarif = sarif.String()
	}
	if req.format == formatSARIF {
		out.WriteString(result.sarif)
	} else if req.format != formatJSON && req.format != formatJSONLines {
		rendered, err := renderAnalysis(req.format, analysis, req.workingDir, workspace)
		if err != nil {
			result.lintOutcome = &lintOutcome{"", err.Error() + "\n", actionlint.ExitStatusFailure}
			return result
		}
		out.WriteString(rendered)
	}
	session.Completed(analysis)
	if len(analysis.Diagnostics) > 0 {
		result.hints = append(result.hints, quotedIgnoreHints(req.ignore, analysis.Diagnostics)...)
		result.lintOutcome = &lintOutcome{out.String(), logs.String(), actionlint.ExitStatusSuccessProblemFound}
		return result
	}
	result.lintOutcome = &lintOutcome{out.String(), logs.String(), actionlint.ExitStatusSuccessNoProblem}
	return result
}

// Rebase SARIF and GitHub reports without changing source metadata or the
// analysis-relative paths used by persisted diagnostics and other formats.
func workspaceReportAnalysis(analysis *actionlint.AnalysisResult, workingDir, workspace string) *actionlint.AnalysisResult {
	result := *analysis
	result.Diagnostics = slices.Clone(analysis.Diagnostics)
	for i := range result.Diagnostics {
		diagnostic := &result.Diagnostics[i]
		diagnostic.Path = reportPath(workingDir, workspace, diagnostic.Path)
		diagnostic.Fixes = slices.Clone(diagnostic.Fixes)
		for j := range diagnostic.Fixes {
			fix := &diagnostic.Fixes[j]
			fix.Edits = slices.Clone(fix.Edits)
			for k := range fix.Edits {
				fix.Edits[k].Path = reportPath(workingDir, workspace, fix.Edits[k].Path)
			}
		}
	}
	return &result
}

// Reports use workspace-relative paths where possible. Files on another Windows
// volume retain their absolute path, which SARIF renders as a file URI.
func reportPath(workingDir, workspace, path string) string {
	absolute := inputPath(workingDir, path)
	if relative, err := filepath.Rel(workspace, absolute); err == nil {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(absolute)
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
	var result *lintResult
	select {
	case result = <-completed:
		if ctx.Err() == nil {
			return result
		}
	case <-ctx.Done():
		timer := time.NewTimer(lintCancellationGrace)
		defer timer.Stop()
		select {
		case result = <-completed:
		case <-timer.C:
		}
	}
	// The analysis owns its buffers until it returns, even if it ignores cancellation.
	if result == nil {
		result = &lintResult{}
	}
	msg := fmt.Sprintf("actionlint timed out after %d seconds\n", int(a.timeout.Seconds()))
	result.lintOutcome = &lintOutcome{"", msg, actionlint.ExitStatusFailure}
	return result
}
