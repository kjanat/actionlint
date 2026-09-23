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
		workingDir:   filepath.Join(workspaceDir, workingRel),
		workspaceDir: workspaceDir,
		ignore:       in.ignore,
		format:       in.format,
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
		path := f
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingRel, path)
		}
		if _, err := workspaceRel(workspaceDir, path, "files"); err != nil {
			return nil, err
		}
	}
	req.files = in.files
	return req, nil
}

func runLinter(req *lintRequest) *lintResult {
	var out, logs bytes.Buffer
	result := &lintResult{}
	workspace := req.workspaceDir
	if workspace == "" {
		workspace = req.workingDir
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		result.lintOutcome = &lintOutcome{"", err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}
	defer root.Close()
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
		ReadFile:           workspaceReader(root, workspace),
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
	sarifAnalysis, err := workspaceSARIFAnalysis(analysis, req.workingDir, workspace)
	if err != nil {
		result.lintOutcome = &lintOutcome{"", err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}
	selected := analysis
	if req.format == formatSARIF {
		selected = sarifAnalysis
	}
	if err := renderer.Render(&out, selected); err != nil {
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}
	result.diagnostics = analysis.Diagnostics
	var sarif bytes.Buffer
	sarifRenderer, err := actionlint.NewAnalysisRenderer(actionlint.OutputFormatSARIF, "", false)
	if err == nil {
		err = sarifRenderer.Render(&sarif, sarifAnalysis)
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

// Copy the complete result to retain renderer rule/source metadata while keeping
// persisted diagnostics and non-SARIF formats relative to the analysis directory.
func workspaceSARIFAnalysis(analysis *actionlint.AnalysisResult, workingDir, workspace string) (*actionlint.AnalysisResult, error) {
	rebase := func(path string) (string, error) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		relative, err := filepath.Rel(workspace, path)
		return filepath.ToSlash(relative), err
	}
	result := *analysis
	result.Diagnostics = slices.Clone(analysis.Diagnostics)
	for i := range result.Diagnostics {
		diagnostic := &result.Diagnostics[i]
		var err error
		diagnostic.Path, err = rebase(diagnostic.Path)
		if err != nil {
			return nil, fmt.Errorf("rebase SARIF diagnostic path: %w", err)
		}
		diagnostic.Fixes = slices.Clone(diagnostic.Fixes)
		for j := range diagnostic.Fixes {
			fix := &diagnostic.Fixes[j]
			fix.Edits = slices.Clone(fix.Edits)
			for k := range fix.Edits {
				fix.Edits[k].Path, err = rebase(fix.Edits[k].Path)
				if err != nil {
					return nil, fmt.Errorf("rebase SARIF fix path: %w", err)
				}
			}
		}
	}
	return &result, nil
}

func workspaceReader(root *os.Root, workspace string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		rel, err := workspaceRel(workspace, path, "files")
		if err != nil {
			return nil, &os.PathError{Op: "read", Path: path, Err: errors.New("path is outside the repository workspace")}
		}
		return root.ReadFile(rel)
	}
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
