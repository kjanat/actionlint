package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kjanat/actionlint"
)

type namedOutput struct {
	name  string
	value string
}

func newDelimiter() string {
	return "actionlint_" + rand.Text()
}

func (a *action) appendOutputs(values []namedOutput) error {
	path := a.env("GITHUB_OUTPUT")
	if path == "" {
		return nil
	}
	var b strings.Builder
	for _, v := range values {
		delimiter := a.newID()
		for slices.Contains(strings.Split(v.value, "\n"), delimiter) {
			delimiter += "_"
		}
		fmt.Fprintf(&b, "%s<<%s\n%s", v.name, delimiter, v.value)
		if v.value != "" && !strings.HasSuffix(v.value, "\n") {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s\n", delimiter)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeResultFile(root *os.Root, target, content string) (string, error) {
	if target == "" {
		return "", nil
	}

	missing := []string{}
	for parent := filepath.Dir(target); parent != "."; parent = filepath.Dir(parent) {
		if _, err := root.Stat(parent); err == nil {
			break
		}
		missing = append(missing, parent)
	}
	if dir := filepath.Dir(target); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	f, err := root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	slices.Reverse(missing)
	if err := inheritOwner(root, append(missing, target)); err != nil {
		return "", err
	}
	return filepath.ToSlash(target), nil
}

func (a *action) emit(rendered string, code int, format outputFormat) {
	if rendered == "" {
		return
	}
	if code == actionlint.ExitStatusInvalidCommandOption || code == actionlint.ExitStatusFailure {
		fmt.Fprintf(a.stdout, "::error title=actionlint failed::%s\n", commandEscape(rendered))
		return
	}
	if format == formatGitHub {
		fmt.Fprint(a.stdout, rendered)
		return
	}

	token := a.newID()
	fmt.Fprintf(a.stdout, "::stop-commands::%s\n", token)
	fmt.Fprint(a.stdout, rendered)
	if !strings.HasSuffix(rendered, "\n") {
		fmt.Fprintln(a.stdout)
	}
	fmt.Fprintf(a.stdout, "::%s::\n", token)
}
