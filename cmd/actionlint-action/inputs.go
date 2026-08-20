package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type outputFormat string

const (
	formatGitHub    outputFormat = "github"
	formatDefault   outputFormat = "default"
	formatOneline   outputFormat = "oneline"
	formatJSON      outputFormat = "json"
	formatJSONLines outputFormat = "json-lines"
	formatMarkdown  outputFormat = "markdown"
	formatSARIF     outputFormat = "sarif"
)

var formats = []outputFormat{
	formatGitHub,
	formatDefault,
	formatOneline,
	formatJSON,
	formatJSONLines,
	formatMarkdown,
	formatSARIF,
}

type inputError struct {
	message string
}

func (e *inputError) Error() string {
	return e.message
}

func inputErrorf(format string, args ...any) error {
	return &inputError{fmt.Sprintf(format, args...)}
}

type inputs struct {
	files            []string
	format           outputFormat
	ignore           []string
	configFile       string
	shellcheck       bool
	pyflakes         bool
	workingDirectory string
	outputFile       string
	failOnError      bool
}

func splitLines(value string) []string {
	ret := []string{}
	for l := range strings.SplitSeq(value, "\n") {
		if l = strings.TrimSuffix(l, "\r"); l != "" {
			ret = append(ret, l)
		}
	}
	return ret
}

func parseBool(name, value string) (bool, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, inputErrorf("Input '%s' must be 'true' or 'false'", name)
	}
}

func parseFormat(value string) (outputFormat, error) {
	for _, f := range formats {
		if string(f) == value {
			return f, nil
		}
	}
	return "", inputErrorf("Input 'format' must be github, default, oneline, json, json-lines, markdown, or sarif")
}

func parseInputs(args []string) (*inputs, error) {
	if len(args) != 10 {
		return nil, inputErrorf("The action received an unexpected number of inputs")
	}

	format, err := parseFormat(args[2])
	if err != nil {
		return nil, err
	}
	files := splitLines(args[1])
	for _, f := range files {
		if strings.HasPrefix(f, "-") {
			return nil, inputErrorf("Input 'files' entries must not start with '-'; provide workflow file paths")
		}
	}
	shellcheck, err := parseBool("shellcheck", args[5])
	if err != nil {
		return nil, err
	}
	pyflakes, err := parseBool("pyflakes", args[6])
	if err != nil {
		return nil, err
	}
	failOnError, err := parseBool("fail-on-error", args[9])
	if err != nil {
		return nil, err
	}

	return &inputs{
		files:            files,
		format:           format,
		ignore:           splitLines(args[3]),
		configFile:       args[4],
		shellcheck:       shellcheck,
		pyflakes:         pyflakes,
		workingDirectory: args[7],
		outputFile:       args[8],
		failOnError:      failOnError,
	}, nil
}

func resolvePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = filepath.Join(wd, path)
	}
	path = filepath.Clean(path)

	rest := ""
	head := path
	for {
		resolved, err := filepath.EvalSymlinks(head)
		if err == nil {
			if rest == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(head)
		if parent == head {
			return path, nil
		}
		rest = filepath.Join(filepath.Base(head), rest)
		head = parent
	}
}

func workspaceRel(workspaceDir, value, name string) (string, error) {
	rel := filepath.Clean(value)
	if filepath.IsAbs(rel) {
		r, err := filepath.Rel(workspaceDir, rel)
		if err != nil {
			return "", inputErrorf("Input '%s' must stay within the repository workspace", name)
		}
		rel = r
	}
	if rel == "." {
		return ".", nil
	}
	if !filepath.IsLocal(rel) {
		return "", inputErrorf("Input '%s' must stay within the repository workspace", name)
	}
	return rel, nil
}

func workingDirectory(root *os.Root, workspaceDir, value string) (string, error) {
	rel, err := workspaceRel(workspaceDir, value, "working-directory")
	if err != nil {
		return "", err
	}
	if s, err := root.Stat(rel); err != nil || !s.IsDir() {
		return "", inputErrorf("Input 'working-directory' must identify an existing directory")
	}
	return rel, nil
}

func resolveOutputFile(root *os.Root, workspaceDir, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	rel, err := workspaceRel(workspaceDir, value, "output-file")
	if err != nil {
		return "", err
	}
	if s, err := root.Stat(rel); err == nil && s.IsDir() {
		return "", inputErrorf("Input 'output-file' must not identify a directory")
	}
	return rel, nil
}
