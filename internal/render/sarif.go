package render

import (
	"encoding/json"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/diagnostic"
)

type sarifDocument struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	ColumnKind string        `json:"columnKind"`
	Tool       sarifTool     `json:"tool"`
	Results    []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name"`
	DefaultConfiguration sarifLevel          `json:"defaultConfiguration"`
	Properties           sarifRuleProperties `json:"properties"`
	FullDescription      sarifText           `json:"fullDescription"`
	HelpURI              string              `json:"helpUri,omitempty"`
	Help                 *sarifText          `json:"help,omitempty"`
}

type sarifLevel struct {
	Level string `json:"level"`
}

type sarifRuleProperties struct {
	Description string `json:"description"`
	QueryURI    string `json:"queryURI"`
}

type sarifResult struct {
	RuleID           string                 `json:"ruleId"`
	Message          sarifText              `json:"message"`
	Level            string                 `json:"level"`
	Properties       *sarifResultProperties `json:"properties,omitempty"`
	Fixes            []sarifFix             `json:"fixes,omitempty"`
	Locations        []sarifLocation        `json:"locations"`
	RelatedLocations []sarifRelatedLocation `json:"relatedLocations,omitempty"`
}

type sarifResultProperties struct {
	ExternalCode string `json:"externalCode"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifRelatedLocation struct {
	ID               int                   `json:"id"`
	Message          sarifText             `json:"message"`
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifRegion struct {
	StartLine   int        `json:"startLine"`
	StartColumn int        `json:"startColumn"`
	EndLine     int        `json:"endLine"`
	EndColumn   int        `json:"endColumn"`
	Snippet     *sarifText `json:"snippet,omitempty"`
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

func sarifSeverityLevel(severity string) string {
	switch severity {
	case "warning":
		return "warning"
	case "info", "style":
		return "note"
	default:
		return "error"
	}
}

func sarifFixes(fixes []diagnostic.Fix) []sarifFix {
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
				DeletedRegion:   sarifRegion{StartLine: edit.Start.Line, StartColumn: edit.Start.Column, EndLine: edit.End.Line, EndColumn: edit.End.Column},
				InsertedContent: sarifText{edit.Replacement},
			})
		}
		result = append(result, converted)
	}
	return result
}

func sarifRules(rules []*Rule) []sarifRule {
	result := make([]sarifRule, 0, len(rules))
	for _, rule := range rules {
		description := rule.Explanation
		if description == "" {
			description = rule.Description
		}
		var help *sarifText
		if rule.Help != "" {
			help = &sarifText{rule.Help}
		}
		result = append(result, sarifRule{
			ID: rule.Name, Name: toPascalCase(rule.Name), DefaultConfiguration: sarifLevel{"error"},
			Properties:      sarifRuleProperties{Description: rule.Description, QueryURI: rule.DocumentationURL},
			FullDescription: sarifText{description}, HelpURI: rule.DocumentationURL, Help: help,
		})
	}
	return result
}

// WriteSARIF encodes diagnostics as a SARIF 2.1.0 log with Unicode code point columns
// and exclusive range ends.
func WriteSARIF(out io.Writer, version string, rules []*Rule, diagnostics []diagnostic.Diagnostic) error {
	results := make([]sarifResult, 0, len(diagnostics))
	for _, d := range diagnostics {
		artifact := sarifArtifact(d.Path)
		result := sarifResult{
			RuleID: d.Rule, Message: sarifText{d.Message}, Level: sarifSeverityLevel(d.Severity), Fixes: sarifFixes(d.Fixes),
			Locations: []sarifLocation{{PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: artifact,
				Region:           sarifRegion{StartLine: d.Start.Line, StartColumn: d.Start.Column, EndLine: d.End.Line, EndColumn: d.End.Column, Snippet: &sarifText{d.Snippet}},
			}}},
		}
		for i, related := range d.Related {
			result.RelatedLocations = append(result.RelatedLocations, sarifRelatedLocation{
				ID: i, Message: sarifText{related.Message},
				PhysicalLocation: sarifPhysicalLocation{ArtifactLocation: sarifArtifact(related.Path), Region: sarifRegion{StartLine: related.Start.Line, StartColumn: related.Start.Column, EndLine: related.End.Line, EndColumn: related.End.Column}},
			})
		}
		if d.Code != "" {
			result.Properties = &sarifResultProperties{ExternalCode: d.Code}
		}
		if len(result.Fixes) == 0 {
			result.Fixes = nil
		}
		results = append(results, result)
	}
	document := sarifDocument{
		Schema: "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json", Version: "2.1.0",
		Runs: []sarifRun{{
			ColumnKind: "unicodeCodePoints",
			Tool:       sarifTool{Driver: sarifDriver{Name: "GitHub Actions lint", Version: version, InformationURI: "https://github.com/kjanat/actionlint", Rules: sarifRules(rules)}},
			Results:    results,
		}},
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}
