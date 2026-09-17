package githubaction

import (
	"bytes"
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
	workingDir string
	configFile string
	overlays   []actionlint.ConfigOverlay
	ignore     []string
	shellcheck string
	pyflakes   string
	format     string
	files      []string
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
		req.shellcheck = command
	}
	if command := env("ACTIONLINT_PYFLAKES_COMMAND"); req.pyflakes != "" && command != "" {
		req.pyflakes = command
	}
	python, script := env("ACTIONLINT_PYTHON"), env("ACTIONLINT_PYFLAKES_SCRIPT")
	if req.pyflakes != "" && python != "" && script != "" {
		req.pyflakes = quoteCommandArgument(python) + " -I " + quoteCommandArgument(script)
	}
	return nil
}

// The external process resolver parses shell words without invoking a shell.
func quoteCommandArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
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
	opts := &actionlint.LinterOptions{
		Color:          actionlint.ColorOptionKindNever,
		Shellcheck:     req.shellcheck,
		Pyflakes:       req.pyflakes,
		IgnorePatterns: req.ignore,
		ConfigFile:     req.configFile,
		ConfigOverlays: req.overlays,
		Format:         req.format,
		WorkingDir:     req.workingDir,
		LogWriter:      &logs,
		OnConfigLoaded: func(report actionlint.ConfigReport) {
			result.configs = append(result.configs, report)
		},
		OnFilesSelected: func(files []string) {
			result.fileCount = len(files)
			result.fileCountKnown = true
		},
	}

	l, err := actionlint.NewLinter(&out, opts)
	if err != nil {
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}

	var errs []*actionlint.Error
	if len(req.files) == 0 {
		errs, err = l.LintRepository(req.workingDir)
	} else {
		errs, err = l.LintFiles(req.files, nil)
	}
	if err != nil {
		code := actionlint.ExitStatusFailure
		if _, ok := errors.AsType[*actionlint.ConfigOverlayError](err); ok {
			code = actionlint.ExitStatusInvalidCommandOption
		}
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", code}
		return result
	}
	if len(errs) > 0 {
		result.hints = quotedIgnoreHints(req.ignore, errs)
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

	done := make(chan *lintResult, 1)
	go func() {
		done <- a.lint(req)
	}()
	select {
	case o := <-done:
		return o
	case <-time.After(a.timeout):
		msg := fmt.Sprintf("actionlint timed out after %d seconds\n", int(a.timeout.Seconds()))
		return &lintResult{lintOutcome: &lintOutcome{"", msg, actionlint.ExitStatusFailure}}
	}
}
