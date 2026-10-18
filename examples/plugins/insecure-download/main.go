package main

import (
	"strings"
	"unicode/utf8"

	"example.com/actionlint-plugin-insecure-download/gen/kjanat/actionlint/analyzer"
	"go.bytecodealliance.org/cm"
)

func init() {
	analyzer.Exports.Describe = describe
	analyzer.Exports.Analyze = analyze
}

func main() {}

func describe() analyzer.Descriptor {
	return analyzer.Descriptor{
		Name: "insecure-download", Description: "Detect disabled TLS verification", Category: "security", Subcategory: "scripts", Maturity: "stable", Provider: "example", Explanation: "Disabling TLS verification permits an intercepted download.", Help: "Remove the insecure flag and configure trusted certificates.", FixAvailability: "none", OptionsSchema: "{}",
		Requires: cm.ToList([]analyzer.Fact{analyzer.FactScripts, analyzer.FactInvocations}),
	}
}

func analyze(input analyzer.Input) analyzer.Outcome {
	var findings []analyzer.Diagnostic
	for _, script := range input.Scripts.Slice() {
		findings = append(findings, checkScript(script)...)
	}
	for _, script := range input.Invoked.Slice() {
		findings = append(findings, checkScript(script)...)
	}
	return analyzer.Outcome{Diagnostics: cm.ToList(findings)}
}

type token struct {
	column uint32
	text   string
}

func checkScript(script analyzer.Script) []analyzer.Diagnostic {
	var findings []analyzer.Diagnostic
	for index, line := range strings.Split(script.Text, "\n") {
		lineNumber := uint32(index + 1)
		for _, flag := range insecureFlags(line) {
			start, ok := position(script, lineNumber, flag.column, false)
			if !ok {
				continue
			}
			end, ok := position(script, lineNumber, flag.column+uint32(utf8.RuneCountInString(flag.text)), true)
			if !ok {
				end = start
			}
			findings = append(findings, analyzer.Diagnostic{
				Reference: cm.Some(script.Reference),
				Severity:  "warning",
				Message:   "step disables TLS verification with " + flag.text + " for a download",
				Path:      script.Path,
				Start:     start,
				End:       end,
			})
		}
	}
	return findings
}

func insecureFlags(line string) []token {
	var flags []token
	var command string
	for _, segment := range commandSegments(line) {
		command = ""
		for _, word := range segment {
			if command == "" {
				if word.text == "sudo" || strings.Contains(word.text, "=") {
					continue
				}
				command = word.text
				continue
			}
			switch {
			case command == "curl" && (word.text == "--insecure" || isShortFlagWith(word.text, 'k')):
				flags = append(flags, word)
			case command == "wget" && word.text == "--no-check-certificate":
				flags = append(flags, word)
			}
		}
	}
	return flags
}

func isShortFlagWith(word string, flag rune) bool {
	return len(word) > 1 && word[0] == '-' && word[1] != '-' && strings.ContainsRune(word[1:], flag)
}

func commandSegments(line string) [][]token {
	var segments [][]token
	var current []token
	var word strings.Builder
	wordStart := uint32(0)
	column := uint32(1)
	flush := func() {
		if word.Len() > 0 {
			current = append(current, token{column: wordStart, text: word.String()})
			word.Reset()
		}
	}
	for _, r := range line {
		switch r {
		case ' ', '\t':
			flush()
		case '|', ';', '&':
			flush()
			segments = append(segments, current)
			current = nil
		default:
			if word.Len() == 0 {
				wordStart = column
			}
			word.WriteRune(r)
		}
		column++
	}
	flush()
	return append(segments, current)
}

func position(script analyzer.Script, line, column uint32, exclusive bool) (analyzer.Position, bool) {
	for _, span := range script.Spans.Slice() {
		if span.Line != line {
			continue
		}
		inside := column >= span.Start && column < span.End
		if exclusive {
			inside = column > span.Start && column <= span.End || span.Start == span.End && column == span.Start
		}
		if !inside {
			continue
		}
		if documentEnd := span.DocumentEnd.Some(); documentEnd != nil {
			if exclusive && column == span.End {
				return *documentEnd, true
			}
			return span.Document, true
		}
		return analyzer.Position{Line: span.Document.Line, Column: span.Document.Column + column - span.Start}, true
	}
	return analyzer.Position{}, false
}
