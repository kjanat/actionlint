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

	documentmodel "actionlint.kjanat.dev/document"

	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/tools/adapters"

	repoconfig "actionlint.kjanat.dev/internal/config"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/buildinfo"
	toolconfig "actionlint.kjanat.dev/internal/config/tools"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/render"
)

const lintTimeout = 300 * time.Second

// Allow canceled analysis to drain callbacks and hand back its partial result.
const lintCancellationGrace = time.Second

type lintRequest struct {
	ruff               string
	ruffOptions        *processconfig.Options
	ctx                context.Context
	shellcheckOptions  *processconfig.Options
	shellcheckSettings *toolconfig.ShellcheckSettings
	workingDir         string
	workspaceDir       string
	configFile         string
	overlays           []repoconfig.Overlay
	ignore             []string
	shellcheck         string
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
	diagnostics    []diagnostic.Diagnostic
	documents      documentmodel.Outlines
	sarif          string
	fileCount      int
	fileCountKnown bool
	configs        []repoconfig.Report
	hints          []string
}

func (req *lintRequest) configureEnvironment(env func(string) string) error {
	// The launcher provisions enabled Ruff before invoking this native entrypoint.
	ruff := env("INPUT_RUFF")
	if ruff != "" {
		enabled, err := parseBool("ruff", ruff)
		if err != nil {
			return err
		}
		if enabled {
			req.ruff = "ruff"
		}
	}
	if command := env("ACTIONLINT_RUFF_COMMAND"); req.ruff != "" && command != "" {
		req.ruffOptions = &processconfig.Options{Executable: &command}
	}
	req.sarif = env("INPUT_SARIF") == "true"
	if value := env("INPUT_CONFIG"); strings.TrimSpace(value) != "" {
		overlay, err := repoconfig.ParseOverlay("config", []byte(value))
		if err != nil {
			return inputErrorf("%s", err)
		}
		req.overlays = append(req.overlays, overlay)
	}
	if command := env("ACTIONLINT_SHELLCHECK_COMMAND"); req.shellcheck != "" && command != "" {
		req.shellcheckOptions = &processconfig.Options{Executable: &command}
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
	opts := application.Options{

		IgnorePatterns: req.ignore,
		ConfigFile:     req.configFile,
		ConfigOverlays: req.overlays,
		WorkingDir:     req.workingDir,
		LogWriter:      &logs,
		OnConfigLoaded: func(report repoconfig.Report) {
			result.configs = append(result.configs, report)
		},
		OnFilesSelected: func(files []string) {
			result.fileCount = len(files)
			result.fileCountKnown = true
		}, Tools: adapters.Options{Ruff: req.ruff, RuffOptions: toolOptionsInDirectory(req.ruffOptions, req.workingDir),
			ShellcheckOptions:  toolOptionsInDirectory(req.shellcheckOptions, req.workingDir),
			ShellcheckSettings: req.shellcheckSettings,
			ShellCheck:         req.shellcheck}.Registrations(),
	}

	ctx := req.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var analysis *application.Result
	var err error
	inputNames := make(map[string]string, len(req.files))
	if len(req.files) == 0 {
		analysis, err = application.Check(ctx, opts, application.Input{Kind: application.Repository, Directory: req.workingDir})
	} else {
		paths := make([]string, len(req.files))
		for i, path := range req.files {
			if !filepath.IsAbs(path) {
				inputNames[filepath.Clean(path)] = path
				path = filepath.Join(req.workingDir, path)
			}
			paths[i] = path
		}
		analysis, err = application.Check(ctx, opts, application.Input{Kind: application.Files, Paths: paths})
	}
	// Absolute reads must retain the caller's relative spelling in Action output.
	if analysis != nil {
		for i := range analysis.Documents {
			if path, ok := inputNames[analysis.Documents[i].DocumentPath()]; ok {
				analysis.Documents[i] = analysis.Documents[i].WithPath(path)
			}
		}
		result.documents = analysis.Documents
		result.fileCount = analysis.FileCount
		result.fileCountKnown = true
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
		code := render.ExitFailure
		if _, ok := errors.AsType[*repoconfig.OverlayError](err); ok {
			code = render.ExitInvalidCommandOption
		}
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", code}
		return result
	}
	if req.format == formatSARIF || req.sarif {
		var sarif bytes.Buffer
		sarifRenderer, err := render.New(render.FormatSARIF, "", false, buildinfo.Version, nil)
		if err == nil {
			err = sarifRenderer.Render(&sarif, render.Prepare(*workspaceReportAnalysis(analysis, req.workingDir, workspace)))
		}
		if err != nil {
			result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", render.ExitFailure}
			return result
		}
		result.sarif = sarif.String()
	}
	if req.format == formatSARIF {
		out.WriteString(result.sarif)
	} else if req.format != formatJSON && req.format != formatJSONLines {
		rendered, err := renderAnalysis(req.format, analysis, req.workingDir, workspace)
		if err != nil {
			result.lintOutcome = &lintOutcome{"", err.Error() + "\n", render.ExitFailure}
			return result
		}
		out.WriteString(rendered)
	}
	if len(analysis.Diagnostics) > 0 {
		result.hints = append(result.hints, quotedIgnoreHints(req.ignore, analysis.Diagnostics)...)
		result.lintOutcome = &lintOutcome{out.String(), logs.String(), render.ExitSuccessProblemFound}
		return result
	}
	result.lintOutcome = &lintOutcome{out.String(), logs.String(), render.ExitSuccessNoProblem}
	return result
}

// Rebase SARIF and GitHub reports without changing source metadata or the
// analysis-relative paths used by persisted diagnostics and other formats.
func workspaceReportAnalysis(analysis *application.Result, workingDir, workspace string) *application.Result {
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

func toolOptionsInDirectory(options *processconfig.Options, directory string) *processconfig.Options {
	configured := processconfig.Options{}
	if options != nil {
		configured = *options
	}
	configured.WorkingDir = directory
	return &configured
}

func (a *action) runLint(req *lintRequest) *lintResult {
	info, err := os.Stat(req.workingDir)
	if err != nil {
		return &lintResult{lintOutcome: &lintOutcome{"", err.Error() + "\n", render.ExitFailure}}
	}
	if !info.IsDir() {
		msg := fmt.Sprintf("working directory %q is not a directory\n", req.workingDir)
		return &lintResult{lintOutcome: &lintOutcome{"", msg, render.ExitFailure}}
	}

	withTimeout := a.withTimeout
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	ctx, cancel := withTimeout(context.Background(), a.timeout)
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
	result.lintOutcome = &lintOutcome{"", msg, render.ExitFailure}
	return result
}
