package process

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
)

// SARIF decodes a SARIF 2.1.0 log. Results of every run become diagnostics;
// suppressed results and results whose kind is not "fail" are skipped.
// Columns are passed through as the log reports them; the manifest's output
// coordinates describe their unit.
type SARIF struct{}

type sarifLog struct {
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool struct {
		Driver struct {
			Rules []sarifRule `json:"rules"`
		} `json:"driver"`
	} `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifRule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	HelpURI string `json:"helpUri"`
}

type sarifResult struct {
	RuleID    string `json:"ruleId"`
	RuleIndex *int   `json:"ruleIndex"`
	Rule      *struct {
		ID    string `json:"id"`
		Index *int   `json:"index"`
	} `json:"rule"`
	Kind    string `json:"kind"`
	Level   string `json:"level"`
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
	Locations []struct {
		PhysicalLocation struct {
			ArtifactLocation struct {
				URI string `json:"uri"`
			} `json:"artifactLocation"`
			Region struct {
				StartLine   int `json:"startLine"`
				StartColumn int `json:"startColumn"`
				EndLine     int `json:"endLine"`
				EndColumn   int `json:"endColumn"`
			} `json:"region"`
		} `json:"physicalLocation"`
	} `json:"locations"`
	Suppressions []struct{} `json:"suppressions"`
}

// Decode implements Decoder. A result whose artifact is not vars["filename"]
// (or vars["filename_abs"]) is a finding in a file the engine read: its
// Filename and Source are the artifact URI.
func (SARIF) Decode(ctx context.Context, stdout []byte, vars map[string]string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := checkInput(stdout); err != nil {
		return Result{}, err
	}
	var log sarifLog
	if err := jsonv2.Unmarshal(stdout, &log); err != nil {
		return Result{}, fmt.Errorf("decode SARIF log: %w", err)
	}
	if log.Version != "2.1.0" {
		return Result{}, fmt.Errorf("unsupported SARIF version %q; expected 2.1.0", log.Version)
	}
	var result Result
	for _, run := range log.Runs {
		for _, entry := range run.Results {
			if len(entry.Suppressions) != 0 || entry.Kind != "" && entry.Kind != "fail" {
				continue
			}
			diagnostic, err := sarifDiagnostic(run.Tool.Driver.Rules, entry, vars)
			if err != nil {
				return Result{}, err
			}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			if err := checkCount(len(result.Diagnostics)); err != nil {
				return Result{}, err
			}
		}
	}
	return result, nil
}

func sarifDiagnostic(rules []sarifRule, entry sarifResult, vars map[string]string) (Diagnostic, error) {
	descriptor := sarifResultRule(rules, entry)
	code := entry.RuleID
	if code == "" && entry.Rule != nil {
		code = entry.Rule.ID
	}
	if code == "" {
		code = descriptor.ID
	}
	if code == "" || entry.Message.Text == "" || len(entry.Locations) == 0 {
		return Diagnostic{}, errors.New("SARIF result needs a rule id, message text and a location")
	}
	location := entry.Locations[0].PhysicalLocation
	region := location.Region
	if region.StartLine < 1 {
		return Diagnostic{}, fmt.Errorf("SARIF result %s has no line region", code)
	}
	level := entry.Level
	if level == "" {
		level = "warning"
	}
	d := Diagnostic{
		Code: code, Name: descriptor.Name, Message: entry.Message.Text, Severity: level, URL: descriptor.HelpURI,
		Location: analyzer.ScriptPosition{Row: region.StartLine, Column: max(region.StartColumn, 1)},
	}
	if region.EndColumn > 0 {
		d.EndLocation = analyzer.ScriptPosition{Row: max(region.EndLine, region.StartLine), Column: region.EndColumn}
	}
	uri := location.ArtifactLocation.URI
	if sameArtifact(uri, vars["filename"]) || sameArtifact(uri, vars["filename_abs"]) {
		d.Filename = vars["filename"]
	} else {
		d.Filename, d.Source = uri, uri
	}
	return d, nil
}

// sarifResultRule finds the driver rule a result references by index or id.
func sarifResultRule(rules []sarifRule, entry sarifResult) sarifRule {
	index := entry.RuleIndex
	if index == nil && entry.Rule != nil {
		index = entry.Rule.Index
	}
	if index != nil && *index >= 0 && *index < len(rules) {
		return rules[*index]
	}
	id := entry.RuleID
	if id == "" && entry.Rule != nil {
		id = entry.Rule.ID
	}
	for _, rule := range rules {
		if id != "" && rule.ID == id {
			return rule
		}
	}
	return sarifRule{}
}

// sameArtifact reports whether a SARIF artifact URI names path, either
// verbatim or as a file URI.
func sameArtifact(uri, path string) bool {
	if uri == "" || path == "" {
		return false
	}
	if uri == path {
		return true
	}
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return false
	}
	name := parsed.Path
	// file:///C:/dir/name carries a Windows drive after the leading slash.
	if len(name) >= 3 && name[0] == '/' && name[2] == ':' {
		name = name[1:]
	}
	if filepath.Separator == '\\' {
		// Windows paths compare case-insensitively with either separator.
		return strings.EqualFold(filepath.ToSlash(name), filepath.ToSlash(path))
	}
	return name == path
}
