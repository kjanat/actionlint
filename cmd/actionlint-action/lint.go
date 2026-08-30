package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"actionlint.kjanat.dev"
)

const lintTimeout = 300 * time.Second

type lintRequest struct {
	workingDir string
	configFile string
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
		Format:         req.format,
		WorkingDir:     req.workingDir,
		LogWriter:      &logs,
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
		result.lintOutcome = &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
		return result
	}
	if len(errs) > 0 {
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
