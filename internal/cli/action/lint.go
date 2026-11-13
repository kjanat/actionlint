package githubaction

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/integrations/github"

	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/scripts"
	results1 "actionlint.kjanat.dev/v2/internal/results"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

	"actionlint.kjanat.dev/v2/internal/checking"
	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	render "actionlint.kjanat.dev/v2/internal/output"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

const lintTimeout = 300 * time.Second

// Allow canceled analysis to drain callbacks and hand back its partial result.
const lintCancellationGrace = time.Second

type lintRequest struct {
	ctx           context.Context
	remoteActions actions.Provider
	// disabledTools lists the tool ids the disable-tools input turned off.
	disabledTools []string
	// processes replaces the default command of the bundled tools it names.
	// Tests use it to select executables.
	processes    map[string]*processconfig.Options
	workingDir   string
	workspaceDir string
	configFile   string
	overlays     []repoconfig.Overlay
	ignore       []string
	format       outputFormat
	sarif        bool
	files        []string
	hints        []string
	acquisition  plugininstall.Acquisition
}

type lintOutcome struct {
	stdout string
	stderr string
	code   int
}

type lintResult struct {
	*lintOutcome
	report  resultreport.CheckResult
	configs []configreport.Report
}

func (r *lintResult) selectedFileCount() int {
	if r.report.FileCount != nil {
		return *r.report.FileCount
	}
	return 0
}

func (req *lintRequest) configureEnvironment(env func(string) string) error {
	options := github.EnvironmentOptions(github.Online, env, req.acquisition.UserCacheDir)
	options.Transport = req.acquisition.Connections.Transport
	metadata, err := github.New(options)
	if err != nil {
		return inputErrorf("%s", err)
	}
	req.remoteActions = metadata
	req.configureTools(env)
	req.sarif = env("INPUT_SARIF") == "true"
	if value := env("INPUT_CONFIG"); strings.TrimSpace(value) != "" {
		overlay, err := checking.ConfigurationRules().ParseOverlayBeforePlugins("config", []byte(value))
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
	req.files = in.files
	return req
}

func runLinter(req *lintRequest) *lintResult {
	var out, logs bytes.Buffer
	result := &lintResult{report: commandstatus.NewCheckResult(commandstatus.ExitSuccessNoProblem)}
	result.report.Hints = append(result.report.Hints, req.hints...)
	workspace := req.workspaceDir
	if workspace == "" {
		workspace = req.workingDir
	}
	opts := checking.Options{
		RemoteActions:  req.remoteActions,
		IgnorePatterns: req.ignore,
		ConfigFile:     req.configFile,
		ConfigOverlays: req.overlays,
		WorkingDir:     req.workingDir,
		LogWriter:      &logs,
		OnConfigLoaded: func(report configreport.Report) {
			result.configs = append(result.configs, report)
			result.report.AddConfiguration(report)
		},
		OnFilesSelected: func(files []string) {
			result.report.FileCount = new(len(files))
		}, Tools: registry.Registrations(req.toolSelections()),
	}

	ctx := req.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var analysis *checking.Result
	var err error
	inputNames := make(map[string]string, len(req.files))
	input := inputmodel.Request{Kind: inputmodel.Repository, Directory: req.workingDir}
	if len(req.files) != 0 {
		paths := make([]string, len(req.files))
		for i, path := range req.files {
			if !filepath.IsAbs(path) {
				inputNames[filepath.Clean(path)] = path
				path = filepath.Join(req.workingDir, path)
			}
			paths[i] = path
		}
		input = inputmodel.Request{Kind: inputmodel.Files, Paths: paths}
	}
	selection := req.pluginSelection()
	selected, selectionErr := selection.Resolve(ctx, req.workingDir, input)
	if selectionErr != nil {
		err = selectionErr
	} else {
		analysis, err = checking.CheckWithPlugins(ctx, opts, input, selected)
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
		for i := range analysis.Diagnostics {
			analysis.Diagnostics[i] = diagnosticPaths(analysis.Diagnostics[i], restorePath)
		}
		hints := result.report.Hints
		result.report = resultreport.FromAnalysis(*analysis)
		result.report.Hints = hints
	}
	if err != nil {
		code := commandstatus.ExitFailure
		if _, ok := errors.AsType[*repoconfig.OverlayError](err); ok {
			code = commandstatus.ExitInvalidCommandOption
		}
		if _, ok := errors.AsType[*checking.UnknownToolError](err); ok {
			code = commandstatus.ExitInvalidCommandOption
		}
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", code}
		return result
	}
	if req.format == formatSARIF || req.sarif {
		var sarif bytes.Buffer
		sarifRenderer, err := render.New(render.FormatSARIF, "", false, buildinfo.Version, nil)
		if err == nil {
			err = sarifRenderer.Render(&sarif, render.FromAnalysis(*workspaceReportAnalysis(analysis, req.workingDir, workspace)))
		}
		if err != nil {
			result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", commandstatus.ExitFailure}
			return result
		}
		result.report.SARIF = json.RawMessage(sarif.Bytes())
	}
	if req.format == formatSARIF {
		out.Write(result.report.SARIF)
	} else if req.format != formatJSON && req.format != formatJSONLines {
		rendered, err := renderAnalysis(req.format, analysis, req.workingDir, workspace)
		if err != nil {
			result.lintOutcome = &lintOutcome{"", err.Error() + "\n", commandstatus.ExitFailure}
			return result
		}
		out.WriteString(rendered)
	}
	code := commandstatus.AnalysisResult(*analysis).ExitCode
	if code == commandstatus.ExitSuccessProblemFound {
		result.report.Hints = append(result.report.Hints, quotedIgnoreHints(req.ignore, analysis.Diagnostics)...)
	}
	result.lintOutcome = &lintOutcome{out.String(), logs.String(), code}
	return result
}

// Rebase SARIF and GitHub reports without changing source metadata or the
// analysis-relative paths used by persisted diagnostics and other formats.
func workspaceReportAnalysis(analysis *checking.Result, workingDir, workspace string) *checking.Result {
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
func diagnosticPaths(finding results1.Diagnostic, rewrite func(string) string) results1.Diagnostic {
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

// configureTools reads the disable-tools input, a list of tool ids separated
// by newlines or commas.
func (req *lintRequest) configureTools(env func(string) string) {
	req.disabledTools = nil
	for line := range strings.SplitSeq(env("INPUT_DISABLE-TOOLS"), "\n") {
		for id := range strings.SplitSeq(line, ",") {
			if id = strings.TrimSpace(id); id != "" && !slices.Contains(req.disabledTools, id) {
				req.disabledTools = append(req.disabledTools, id)
			}
		}
	}
}

// toolSelections selects every bundled tool: disabled when the inputs
// disabled it, otherwise its default command in the working directory,
// which is skipped with a warning when it cannot run.
func (req *lintRequest) toolSelections() registry.Selections {
	selections := registry.Selections{}
	for _, id := range registry.IDs() {
		if slices.Contains(req.disabledTools, id) {
			selections[id] = scripts.Selection{}
			continue
		}
		options := processconfig.Options{Optional: true}
		if replaced := req.processes[id]; replaced != nil {
			options = *replaced
		}
		options.WorkingDir = req.workingDir
		selections[id] = scripts.Selection{Command: id, Process: &options}
	}
	return selections
}

// requestedTools lists the bundled tools the inputs leave enabled.
func (req *lintRequest) requestedTools() []string {
	var ids []string
	for _, id := range registry.IDs() {
		if !slices.Contains(req.disabledTools, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func (a *action) runLint(req *lintRequest) *lintResult {
	info, err := os.Stat(req.workingDir)
	if err != nil {
		return &lintResult{lintOutcome: &lintOutcome{"", err.Error() + "\n", commandstatus.ExitFailure}}
	}
	if !info.IsDir() {
		msg := fmt.Sprintf("working directory %q is not a directory\n", req.workingDir)
		return &lintResult{lintOutcome: &lintOutcome{"", msg, commandstatus.ExitFailure}}
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
	result.lintOutcome = &lintOutcome{"", msg, commandstatus.ExitFailure}
	return result
}
