package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kjanat/actionlint"
)

const lintTimeout = 300 * time.Second

//go:embed sarif_template.txt
var sarifTemplate string

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

func buildRequest(in *inputs, workspaceDir, workingRel string) (*lintRequest, error) {
	req := &lintRequest{
		workingDir: filepath.Join(workspaceDir, workingRel),
		ignore:     in.ignore,
		format:     "{{json .}}",
	}
	if in.format == formatSARIF {
		req.format = sarifTemplate
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

func runLinter(req *lintRequest) *lintOutcome {
	var out, logs bytes.Buffer
	opts := &actionlint.LinterOptions{
		Color:          actionlint.ColorOptionKindNever,
		Shellcheck:     req.shellcheck,
		Pyflakes:       req.pyflakes,
		IgnorePatterns: req.ignore,
		ConfigFile:     req.configFile,
		Format:         req.format,
		WorkingDir:     req.workingDir,
		LogWriter:      &logs,
	}

	l, err := actionlint.NewLinter(&out, opts)
	if err != nil {
		return &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
	}

	var errs []*actionlint.Error
	if len(req.files) == 0 {
		errs, err = l.LintRepository(req.workingDir)
	} else {
		errs, err = l.LintFiles(req.files, nil)
	}
	if err != nil {
		return &lintOutcome{out.String(), err.Error() + "\n", actionlint.ExitStatusFailure}
	}
	if len(errs) > 0 {
		return &lintOutcome{out.String(), logs.String(), actionlint.ExitStatusSuccessProblemFound}
	}
	return &lintOutcome{out.String(), logs.String(), actionlint.ExitStatusSuccessNoProblem}
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

func (a *action) runLint(req *lintRequest) *lintOutcome {
	restore, err := chdir(req.workingDir)
	if err != nil {
		return &lintOutcome{"", err.Error() + "\n", actionlint.ExitStatusFailure}
	}
	defer restore()

	done := make(chan *lintOutcome, 1)
	go func() {
		done <- a.lint(req)
	}()
	select {
	case o := <-done:
		return o
	case <-time.After(a.timeout):
		msg := fmt.Sprintf("actionlint timed out after %d seconds\n", int(a.timeout.Seconds()))
		return &lintOutcome{"", msg, actionlint.ExitStatusFailure}
	}
}
