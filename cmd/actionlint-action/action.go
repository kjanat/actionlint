package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/kjanat/actionlint"
)

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
	var invalid *inputError
	if errors.As(err, &invalid) {
		fmt.Fprintf(a.stdout, "::error title=Invalid action input::%s\n", commandEscape(err.Error()))
		return actionlint.ExitStatusInvalidCommandOption
	}
	fmt.Fprintf(a.stdout, "::error title=actionlint action failed::%s\n", commandEscape(err.Error()))
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
	workspace, err := a.workspace()
	if err != nil {
		return 0, err
	}
	outputFile, err := resolveOutputFile(workspace, in.outputFile)
	if err != nil {
		return 0, err
	}
	workingDir, err := withinWorkspace(workspace, in.workingDirectory, "working-directory", true)
	if err != nil {
		return 0, err
	}
	req, err := buildRequest(in, workspace, workingDir)
	if err != nil {
		return 0, err
	}

	outcome, count, rendered := renderOutcome(a.runLint(req), in.format)
	result, ok := results[outcome.code]
	if !ok {
		result = "failure"
	}
	written, err := writeResultFile(workspace, outputFile, rendered)
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
