package githubaction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"actionlint.kjanat.dev/internal/buildinfo"
	"actionlint.kjanat.dev/internal/render"
)

func actionVersion() string {
	return buildinfo.Version()
}

type action struct {
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)

	args    []string
	stdout  io.Writer
	env     func(string) string
	lint    func(*lintRequest) *lintResult
	newID   func() string
	timeout time.Duration
	result  *lintResult
}

var results = map[int]string{
	render.ExitSuccessNoProblem:     "success",
	render.ExitSuccessProblemFound:  "problems-found",
	render.ExitInvalidCommandOption: "invalid-options",
	render.ExitFailure:              "failure",
}

func (a *action) run() int {
	code, err := a.execute()
	if err != nil {
		code = render.ExitFailure
		if _, ok := errors.AsType[*inputError](err); ok {
			_, _ = fmt.Fprintf(a.stdout, "::error title=Invalid action input::%s\n", commandEscape(err.Error()))
			code = render.ExitInvalidCommandOption
		} else {
			_, _ = fmt.Fprintf(a.stdout, "::error title=actionlint action failed::%s\n", commandEscape(err.Error()))
		}
	}
	if persistErr := a.persistResult(code, err); persistErr != nil {
		_, _ = fmt.Fprintf(a.stdout, "::error title=Could not save actionlint results::%s\n", commandEscape(persistErr.Error()))
		return render.ExitFailure
	}
	return code
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
	req, err := a.prepareRequest(in, workspaceDir)
	if err != nil {
		return 0, err
	}
	lint := a.runLint(req)
	a.result = lint
	outcome, count := lint.lintOutcome, ""
	var rendered string
	if in.format == formatJSON || in.format == formatJSONLines {
		var output bytes.Buffer
		report := a.checkResult(outcome.code, nil)
		if err := report.WriteJSON(&output, in.format == formatJSONLines); err != nil {
			return 0, err
		}
		rendered = output.String()
		if outcome.code == render.ExitSuccessNoProblem || outcome.code == render.ExitSuccessProblemFound {
			count = strconv.Itoa(len(lint.diagnostics))
		}
	} else {
		outcome, count, rendered = renderOutcome(lint)
	}
	lint.lintOutcome = outcome
	a.emitStatus(outcome.code, count, lint.fileCount, lint.fileCountKnown, req)
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
		{"problems-found", strconv.FormatBool(outcome.code == render.ExitSuccessProblemFound)},
		{"problem-count", count},
		{"output", rendered},
		{"output-file", written},
	})
	if err != nil {
		return 0, err
	}
	if outcome.code >= render.ExitInvalidCommandOption && (in.format == formatJSON || in.format == formatJSONLines) {
		a.emit(outcome.stderr, outcome.code, in.format)
	} else {
		a.emit(rendered, outcome.code, in.format)
	}
	a.emitConfiguration(lint, req.workingDir)

	if outcome.code == render.ExitSuccessProblemFound && !in.failOnError {
		return render.ExitSuccessNoProblem, nil
	}
	return outcome.code, nil
}

func (a *action) prepareRequest(in *inputs, workspaceDir string) (*lintRequest, error) {
	workingDir, err := workingDirectory(workspaceDir, in.workingDirectory)
	if err != nil {
		return nil, err
	}
	req := buildRequest(in, workspaceDir, workingDir)
	if err := req.configureEnvironment(a.env); err != nil {
		return nil, err
	}
	if err := req.configureShellcheck(a.env); err != nil {
		return nil, err
	}
	return req, nil
}
