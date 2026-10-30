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

	configreport "actionlint.kjanat.dev/v2/config/report"
	documentmodel "actionlint.kjanat.dev/v2/document"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/tools/registry"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/buildinfo"
	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/render"
)

const lintTimeout = 300 * time.Second

// Allow canceled analysis to drain callbacks and hand back its partial result.
const lintCancellationGrace = time.Second

type lintRequest struct {
	ruff              string
	ctx               context.Context
	shellcheckOptions *processconfig.Options
	workingDir        string
	workspaceDir      string
	configFile        string
	overlays          []repoconfig.Overlay
	ignore            []string
	shellcheck        string
	format            outputFormat
	sarif             bool
	files             []string
	hints             []string
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
	configs        []configreport.Report
	hints          []string
}

func (req *lintRequest) configureEnvironment(env func(string) string) error {
	// The booleans control managed plugins; explicit native transports belong
	// in the ordinary actionlint configuration.
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
	req.sarif = env("INPUT_SARIF") == "true"
	if value := env("INPUT_CONFIG"); strings.TrimSpace(value) != "" {
		overlay, err := repoconfig.ParseOverlayBeforePlugins("config", []byte(value))
		if err != nil {
			return inputErrorf("%s", err)
		}
		req.overlays = append(req.overlays, overlay)
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
		OnConfigLoaded: func(report configreport.Report) {
			result.configs = append(result.configs, report)
		},
		OnFilesSelected: func(files []string) {
			result.fileCount = len(files)
			result.fileCountKnown = true
		}, Tools: registry.Registrations(registry.Selections{
			"ruff":       {Command: req.ruff, Process: &processconfig.Options{Optional: true}},
			"shellcheck": {Command: req.shellcheck, Process: toolOptionsInDirectory(req.shellcheckOptions, req.workingDir)},
		}),
	}

	ctx := req.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var analysis *application.Result
	var err error
	inputNames := make(map[string]string, len(req.files))
	input := application.Input{Kind: application.Repository, Directory: req.workingDir}
	if len(req.files) != 0 {
		paths := make([]string, len(req.files))
		for i, path := range req.files {
			if !filepath.IsAbs(path) {
				inputNames[filepath.Clean(path)] = path
				path = filepath.Join(req.workingDir, path)
			}
			paths[i] = path
		}
		input = application.Input{Kind: application.Files, Paths: paths}
	}
	selection := req.pluginSelection()
	selectionErr := selection.Migrate(ctx, req.workingDir, input, localapp.DefaultPluginOptions{
		Shellcheck: req.shellcheck == "shellcheck" && req.shellcheckOptions == nil,
		Ruff:       req.ruff == "ruff",
		Warning:    func(message string) { _, _ = fmt.Fprintln(&logs, "warning: "+message) },
	})
	var selected localapp.PluginSources
	if selectionErr == nil {
		selected, selectionErr = selection.Resolve(ctx, req.workingDir, input)
	}
	if selectionErr != nil {
		err = selectionErr
	} else {
		analysis, err = localapp.CheckSources(ctx, opts, input, selected)
	}
	if analysis != nil {
		restorePath := func(path string) string {
			if original, ok := inputNames[filepath.Clean(path)]; ok {
				return original
			}
			return path
		}
		for i := range analysis.Documents {
			if path, ok := inputNames[analysis.Documents[i].DocumentPath()]; ok {
				analysis.Documents[i] = analysis.Documents[i].WithPath(path)
			}
		}
		result.documents = analysis.Documents
		result.fileCount = analysis.FileCount
		result.fileCountKnown = true
		for i := range analysis.Diagnostics {
			analysis.Diagnostics[i] = diagnosticPaths(analysis.Diagnostics[i], restorePath)
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
		result.Diagnostics[i] = diagnosticPaths(result.Diagnostics[i], func(path string) string {
			return reportPath(workingDir, workspace, path)
		})
	}
	return &result
}

// Rewrite every diagnostic path together, preserving revision-to-edit bindings.
func diagnosticPaths(finding diagnostic.Diagnostic, rewrite func(string) string) diagnostic.Diagnostic {
	finding.Path = rewrite(finding.Path)
	finding.Related = slices.Clone(finding.Related)
	for i := range finding.Related {
		finding.Related[i].Path = rewrite(finding.Related[i].Path)
	}
	finding.Fixes = slices.Clone(finding.Fixes)
	for i := range finding.Fixes {
		fix := &finding.Fixes[i]
		fix.Edits = slices.Clone(fix.Edits)
		for j := range fix.Edits {
			fix.Edits[j].Path = rewrite(fix.Edits[j].Path)
		}
		if fix.Revisions != nil {
			revisions := make(map[string]string, len(fix.Revisions))
			for path, revision := range fix.Revisions {
				revisions[rewrite(path)] = revision
			}
			fix.Revisions = revisions
		}
	}
	return finding
}

// Reports use workspace-relative paths where possible and absolute paths for
// files on another Windows volume.
func reportPath(workingDir, workspace, path string) string {
	absolute := inputPath(workingDir, path)
	if relative, err := filepath.Rel(workspace, absolute); err == nil {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(absolute)
}

func toolOptionsInDirectory(options *processconfig.Options, directory string) *processconfig.Options {
	configured := processconfig.Options{Optional: true}
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
