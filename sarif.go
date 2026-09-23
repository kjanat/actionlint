package actionlint

import (
	_ "embed"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
)

//go:embed sarif_template.txt
var sarifTemplate string

// SARIFTemplate returns the canonical Go template for SARIF output.
func SARIFTemplate() string {
	return sarifTemplate
}

// Native SARIF uses exclusive, possibly multiline ranges. Legacy custom templates
// retain their inclusive, single-line ErrorTemplateFields representation.
type sarifTemplateFields struct {
	ErrorTemplateFields
	EndLine int
	Level   string
	Code    string
	Fixes   []sarifFix
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
}

type sarifReplacement struct {
	DeletedRegion   sarifRegion `json:"deletedRegion"`
	InsertedContent sarifText   `json:"insertedContent"`
}

type sarifArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

type sarifArtifactChange struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Replacements     []sarifReplacement    `json:"replacements"`
}

type sarifFix struct {
	Description     sarifText             `json:"description"`
	ArtifactChanges []sarifArtifactChange `json:"artifactChanges"`
}

func sarifArtifact(path string) sarifArtifactLocation {
	slash := filepath.ToSlash(path)
	if filepath.IsAbs(path) {
		if filepath.Separator == '\\' && strings.HasPrefix(slash, "//") {
			host, rest, _ := strings.Cut(slash[2:], "/")
			return sarifArtifactLocation{URI: (&url.URL{Scheme: "file", Host: host, Path: "/" + rest}).String()}
		}
		if !strings.HasPrefix(slash, "/") {
			slash = "/" + slash
		}
		return sarifArtifactLocation{URI: (&url.URL{Scheme: "file", Path: slash}).String()}
	}
	return sarifArtifactLocation{URI: (&url.URL{Path: slash}).String(), URIBaseID: "%SRCROOT%"}
}

func sarifLevel(severity string) string {
	switch severity {
	case "warning":
		return "warning"
	case "info", "style":
		return "note"
	default:
		return "error"
	}
}

func sarifFixes(fixes []DiagnosticFix) []sarifFix {
	result := make([]sarifFix, 0, len(fixes))
	for _, fix := range fixes {
		if len(fix.Edits) == 0 {
			continue
		}
		converted := sarifFix{Description: sarifText{fix.Description}}
		paths := make(map[string]int)
		for _, edit := range fix.Edits {
			index, ok := paths[edit.Path]
			if !ok {
				index = len(converted.ArtifactChanges)
				paths[edit.Path] = index
				converted.ArtifactChanges = append(converted.ArtifactChanges, sarifArtifactChange{ArtifactLocation: sarifArtifact(edit.Path)})
			}
			change := &converted.ArtifactChanges[index]
			change.Replacements = append(change.Replacements, sarifReplacement{
				DeletedRegion:   sarifRegion{edit.Start.Line, edit.Start.Column, edit.End.Line, edit.End.Column},
				InsertedContent: sarifText{edit.Replacement},
			})
		}
		result = append(result, converted)
	}
	return result
}

func (f *ErrorFormatter) printSARIF(out io.Writer, diagnostics []Diagnostic) error {
	fields := make([]sarifTemplateFields, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		fields = append(fields, sarifTemplateFields{
			ErrorTemplateFields: ErrorTemplateFields{
				Message: diagnostic.Message, Filepath: sarifArtifact(diagnostic.Path).URI, Kind: diagnostic.Rule,
				Line: diagnostic.Start.Line, Column: diagnostic.Start.Column,
				EndColumn: diagnostic.End.Column, Snippet: diagnostic.Snippet,
			},
			EndLine: diagnostic.End.Line, Level: sarifLevel(diagnostic.Severity), Code: diagnostic.Code, Fixes: sarifFixes(diagnostic.Fixes),
		})
	}
	if err := f.temp.Execute(out, fields); err != nil {
		return fmt.Errorf("could not format error messages: %w", err)
	}
	return nil
}
