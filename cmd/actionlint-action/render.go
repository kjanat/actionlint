package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kjanat/actionlint"
)

type problem = actionlint.ErrorTemplateFields

func commandEscape(value string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(value)
}

func propertyEscape(value string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(value)
}

func problemHeader(p *problem) string {
	path := p.Filepath
	if strings.HasPrefix(path, "::") {
		path = "./" + path
	}
	return fmt.Sprintf("%s:%d:%d: %s [%s]", path, p.Line, p.Column, p.Message, p.Kind)
}

func renderDefault(problems []*problem) string {
	var b strings.Builder
	for _, p := range problems {
		b.WriteString(problemHeader(p) + "\n")
		if p.Snippet == "" {
			continue
		}
		source, indicator, _ := strings.Cut(p.Snippet, "\n")
		prefix := fmt.Sprintf("%d | ", p.Line)
		indent := strings.Repeat(" ", len(prefix)-2)
		fmt.Fprintf(&b, "%s|\n%s%s\n%s| %s\n", indent, prefix, source, indent, indicator)
	}
	return b.String()
}

func renderGitHub(problems []*problem) string {
	var b strings.Builder
	for _, p := range problems {
		message := p.Message
		if p.Snippet != "" {
			message += "\n\n" + p.Snippet
		}
		fmt.Fprintf(
			&b,
			"::error file=%s,line=%d,col=%d,endColumn=%d,title=%s::%s\n",
			propertyEscape(p.Filepath),
			p.Line,
			p.Column,
			p.EndColumn,
			propertyEscape("actionlint ("+p.Kind+")"),
			commandEscape(message),
		)
	}
	return b.String()
}

func renderOneline(problems []*problem) string {
	var b strings.Builder
	for _, p := range problems {
		b.WriteString(problemHeader(p) + "\n")
	}
	return b.String()
}

func renderJSONLines(problems []*problem) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, p := range problems {
		if err := enc.Encode(p); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func renderMarkdown(problems []*problem) string {
	var b strings.Builder
	for _, p := range problems {
		fmt.Fprintf(&b, "### %s:%d:%d (%s)\n\n%s\n", p.Filepath, p.Line, p.Column, p.Kind, p.Message)
		if p.Snippet != "" {
			b.WriteString("\n")
			for _, line := range strings.Split(strings.TrimSuffix(p.Snippet, "\n"), "\n") {
				b.WriteString("    " + line + "\n")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func render(format outputFormat, problems []*problem, serialized string) (string, error) {
	switch format {
	case formatGitHub:
		return renderGitHub(problems), nil
	case formatDefault:
		return renderDefault(problems), nil
	case formatOneline:
		return renderOneline(problems), nil
	case formatJSON:
		return serialized, nil
	case formatJSONLines:
		return renderJSONLines(problems)
	case formatMarkdown:
		return renderMarkdown(problems), nil
	default:
		return "", fmt.Errorf("format %q has no renderer", format)
	}
}

type sarifDocument struct {
	Runs []struct {
		Results []json.RawMessage `json:"results"`
	} `json:"runs"`
}

func sarifProblemCount(serialized string) (int, error) {
	var doc sarifDocument
	if err := json.Unmarshal([]byte(serialized), &doc); err != nil {
		return 0, err
	}
	count := 0
	for _, run := range doc.Runs {
		count += len(run.Results)
	}
	return count, nil
}

func problemsFromJSON(serialized string) ([]*problem, error) {
	var problems []*problem
	if err := json.Unmarshal([]byte(serialized), &problems); err != nil {
		return nil, err
	}
	return problems, nil
}

func renderOutcome(o *lintOutcome, format outputFormat) (*lintOutcome, string, string) {
	if o.code != actionlint.ExitStatusSuccessNoProblem && o.code != actionlint.ExitStatusSuccessProblemFound {
		rendered := o.stdout
		if o.stderr != "" {
			rendered = o.stderr + o.stdout
		}
		return o, "", rendered
	}

	count, rendered, err := countAndRender(o.stdout, format)
	if err != nil {
		failed := &lintOutcome{o.stdout, fmt.Sprintf("could not parse actionlint output: %s\n", err), actionlint.ExitStatusFailure}
		return failed, "", failed.stderr + failed.stdout
	}
	return o, strconv.Itoa(count), rendered
}

func countAndRender(serialized string, format outputFormat) (int, string, error) {
	if format == formatSARIF {
		count, err := sarifProblemCount(serialized)
		return count, serialized, err
	}
	problems, err := problemsFromJSON(serialized)
	if err != nil {
		return 0, "", err
	}
	out, err := render(format, problems, serialized)
	if err != nil {
		return 0, "", err
	}
	return len(problems), out, nil
}
