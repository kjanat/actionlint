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

func writeResultFile(workspace, target, content string) (string, error) {
	if target == "" {
		return "", nil
	}

	missing := []string{}
	for parent := filepath.Dir(target); parent != workspace; parent = filepath.Dir(parent) {
		if _, err := os.Stat(parent); err == nil {
			break
		}
		missing = append(missing, parent)
		if filepath.Dir(parent) == parent {
			break
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return "", err
	}

	slices.Reverse(missing)
	if err := inheritOwner(workspace, append(missing, target)); err != nil {
		return "", err
	}

	rel, err := filepath.Rel(workspace, target)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
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
