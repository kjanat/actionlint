package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"actionlint.kjanat.dev"
)

var version = ""

func actionVersion() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}
	return info.Main.Version
}

type action struct {
	args    []string
	stdout  io.Writer
	env     func(string) string
	lint    func(*lintRequest) *lintOutcome
	newID   func() string
	timeout time.Duration
}

var results = map[int]string{
	actionlint.ExitStatusSuccessNoProblem:     "success",
	actionlint.ExitStatusSuccessProblemFound:  "problems-found",
	actionlint.ExitStatusInvalidCommandOption: "invalid-options",
	actionlint.ExitStatusFailure:              "failure",
}

func (a *action) run() int {
	code, err := a.execute()
	if err == nil {
		return code
	}
	if _, ok := errors.AsType[*inputError](err); ok {
		_, _ = fmt.Fprintf(a.stdout, "::error title=Invalid action input::%s\n", commandEscape(err.Error()))
		return actionlint.ExitStatusInvalidCommandOption
	}
	_, _ = fmt.Fprintf(a.stdout, "::error title=actionlint action failed::%s\n", commandEscape(err.Error()))
	return actionlint.ExitStatusFailure
}

func (a *action) workspace() (string, error) {
	dir := a.env("GITHUB_WORKSPACE")
	if dir == "" {
		d, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = d
	}
	return resolvePath(dir)
}

func (a *action) execute() (int, error) {
	in, err := parseInputs(a.args)
	if err != nil {
		return 0, err
	}
	workspaceDir, err := a.workspace()
	if err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(workspaceDir)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()

	outputFile, err := resolveOutputFile(root, workspaceDir, in.outputFile)
	if err != nil {
		return 0, err
	}
	workingRel, err := workingDirectory(root, workspaceDir, in.workingDirectory)
	if err != nil {
		return 0, err
	}
	req, err := buildRequest(in, workspaceDir, workingRel)
	if err != nil {
		return 0, err
	}
	fileCount, fileCountErr := workflowFileCount(req)

	outcome, count, rendered := renderOutcome(a.runLint(req), in.format)
	a.emitStatus(outcome.code, count, fileCount, fileCountErr, in)
	result, ok := results[outcome.code]
	if !ok {
		result = "failure"
	}
	written, err := writeResultFile(root, outputFile, rendered)
	if err != nil {
		return 0, err
	}
	err = a.appendOutputs([]namedOutput{
		{"exit-code", strconv.Itoa(outcome.code)},
		{"result", result},
		{"problems-found", strconv.FormatBool(outcome.code == actionlint.ExitStatusSuccessProblemFound)},
		{"problem-count", count},
		{"output", rendered},
		{"output-file", written},
	})
	if err != nil {
		return 0, err
	}
	a.emit(rendered, outcome.code, in.format)

	if outcome.code == actionlint.ExitStatusSuccessProblemFound && !in.failOnError {
		return actionlint.ExitStatusSuccessNoProblem, nil
	}
	return outcome.code, nil
}
