package process

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
)

// SARIF decodes a SARIF 2.1.0 log. Results of every run become diagnostics;
// applied/accepted suppressions and results whose kind is not "fail" are skipped.
// Suppressions under review or rejected remain active findings.
// Columns are passed through as the log reports them; the manifest's output
// coordinates describe their unit.
type SARIF struct{}

type sarifLog struct {
	Version                  string     `json:"version"`
	Runs                     []sarifRun `json:"runs"`
	InlineExternalProperties []struct{} `json:"inlineExternalProperties"`
}

type sarifRun struct {
	ExternalPropertyFileReferences *struct{} `json:"externalPropertyFileReferences"`
	NewlineSequences               []string  `json:"newlineSequences"`
	Tool                           struct {
		Driver     sarifComponent   `json:"driver"`
		Extensions []sarifComponent `json:"extensions"`
	} `json:"tool"`
	Invocations []struct {
		RuleConfigurationOverrides []struct {
			Descriptor    sarifRuleReference `json:"descriptor"`
			Configuration sarifConfiguration `json:"configuration"`
		} `json:"ruleConfigurationOverrides"`
	} `json:"invocations"`
	Results []sarifResult `json:"results"`
}

type sarifComponent struct {
	GUID  string      `json:"guid"`
	Rules []sarifRule `json:"rules"`
}
type sarifConfiguration struct {
	Level string `json:"level"`
}
type sarifRule struct {
	ID                   string             `json:"id"`
	GUID                 string             `json:"guid"`
	Name                 string             `json:"name"`
	HelpURI              string             `json:"helpUri"`
	DefaultConfiguration sarifConfiguration `json:"defaultConfiguration"`
}
type sarifRuleReference struct {
	ID            string `json:"id"`
	GUID          string `json:"guid"`
	Index         *int   `json:"index"`
	ToolComponent *struct {
		Index *int   `json:"index"`
		GUID  string `json:"guid"`
	} `json:"toolComponent"`
}

type sarifResult struct {
	RuleID     string              `json:"ruleId"`
	RuleIndex  *int                `json:"ruleIndex"`
	Rule       *sarifRuleReference `json:"rule"`
	Provenance *struct {
		InvocationIndex *int `json:"invocationIndex"`
	} `json:"provenance"`
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
	Suppressions []sarifSuppression `json:"suppressions"`
}

type sarifSuppression struct {
	Kind   string  `json:"kind"`
	Status *string `json:"status"`
}

func sarifSuppressed(suppressions []sarifSuppression) (bool, error) {
	suppressed := false
	for _, suppression := range suppressions {
		if suppression.Kind != "inSource" && suppression.Kind != "external" {
			return false, fmt.Errorf("invalid SARIF suppression kind %q", suppression.Kind)
		}
		// No review status means the producer applied its native suppression.
		if suppression.Status == nil {
			suppressed = true
			continue
		}
		switch *suppression.Status {
		case "accepted":
			suppressed = true
		case "underReview", "rejected":
		default:
			return false, fmt.Errorf("invalid SARIF suppression status %q", *suppression.Status)
		}
	}
	return suppressed, nil
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
	if len(log.InlineExternalProperties) > 0 {
		return Result{}, errors.New("SARIF inline external properties are unsupported")
	}
	var result Result
	for _, run := range log.Runs {
		if run.ExternalPropertyFileReferences != nil {
			return Result{}, errors.New("SARIF external property files are unsupported")
		}
		if len(run.NewlineSequences) > 0 && (len(run.NewlineSequences) != 2 || run.NewlineSequences[0] != "\r\n" || run.NewlineSequences[1] != "\n") {
			return Result{}, errors.New("SARIF custom newline sequences are unsupported")
		}
		for _, entry := range run.Results {
			suppressed, err := sarifSuppressed(entry.Suppressions)
			if err != nil {
				return Result{}, err
			}
			if suppressed || entry.Kind != "" && entry.Kind != "fail" {
				continue
			}
			diagnostic, err := sarifDiagnostic(run, entry, vars)
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

func sarifDiagnostic(run sarifRun, entry sarifResult, vars map[string]string) (checkinputs.ScriptDiagnostic, error) {
	descriptor, componentIndex, ruleIndex, err := sarifResultRule(run, entry)
	if err != nil {
		return checkinputs.ScriptDiagnostic{}, err
	}
	code := entry.RuleID
	if code == "" && entry.Rule != nil {
		code = entry.Rule.ID
	}
	if code == "" {
		code = descriptor.ID
	}
	if code == "" || entry.Message.Text == "" || len(entry.Locations) == 0 {
		return checkinputs.ScriptDiagnostic{}, errors.New("SARIF result needs a rule id, message text and a location")
	}
	location := entry.Locations[0].PhysicalLocation
	region := location.Region
	if region.StartLine < 1 {
		return checkinputs.ScriptDiagnostic{}, fmt.Errorf("SARIF result %s has no line region", code)
	}
	level := entry.Level
	if level == "" && ruleIndex >= 0 && entry.Provenance != nil && entry.Provenance.InvocationIndex != nil && *entry.Provenance.InvocationIndex >= 0 {
		i := *entry.Provenance.InvocationIndex
		if i >= len(run.Invocations) {
			return checkinputs.ScriptDiagnostic{}, errors.New("SARIF invocation index out of range")
		}
		for _, override := range run.Invocations[i].RuleConfigurationOverrides {
			_, component, rule, err := sarifResultRule(run, sarifResult{Rule: &override.Descriptor})
			if err != nil {
				return checkinputs.ScriptDiagnostic{}, err
			}
			if component == componentIndex && rule == ruleIndex {
				level = override.Configuration.Level
				break
			}
		}
	}
	if level == "" {
		level = descriptor.DefaultConfiguration.Level
	}
	if level == "" {
		level = "warning"
	}
	d := checkinputs.ScriptDiagnostic{
		Code: code, Name: descriptor.Name, Message: entry.Message.Text, Severity: level, URL: descriptor.HelpURI,
		Location: checkinputs.ScriptPosition{Row: region.StartLine, Column: max(region.StartColumn, 1)},
	}
	if region.EndColumn > 0 {
		d.EndLocation = checkinputs.ScriptPosition{Row: max(region.EndLine, region.StartLine), Column: region.EndColumn}
	}
	uri := location.ArtifactLocation.URI
	if sameArtifact(uri, vars["filename"]) || sameArtifact(uri, vars["filename_abs"]) {
		d.Filename = vars["filename"]
	} else {
		d.Filename, d.Source = uri, uri
	}
	return d, nil
}

// sarifResultRule resolves rule references within the selected tool component.
func sarifResultRule(run sarifRun, entry sarifResult) (sarifRule, int, int, error) {
	ref := entry.Rule
	componentIndex := -1
	component := &run.Tool.Driver
	if ref != nil && ref.ToolComponent != nil {
		c := ref.ToolComponent
		if c.Index != nil && *c.Index >= 0 {
			if *c.Index >= len(run.Tool.Extensions) {
				return sarifRule{}, -1, -1, errors.New("SARIF component index out of range")
			}
			componentIndex = *c.Index
			component = &run.Tool.Extensions[componentIndex]
		} else if c.GUID != "" && component.GUID != c.GUID {
			for i := range run.Tool.Extensions {
				if run.Tool.Extensions[i].GUID == c.GUID {
					componentIndex = i
					component = &run.Tool.Extensions[i]
					break
				}
			}
			if componentIndex < 0 {
				return sarifRule{}, -1, -1, errors.New("SARIF component guid not found")
			}
		}
		if c.GUID != "" && c.GUID != component.GUID {
			return sarifRule{}, -1, -1, errors.New("conflicting SARIF component references")
		}
	}
	var index *int
	id := entry.RuleID
	if ref != nil {
		if entry.RuleIndex != nil && ref.Index != nil && *entry.RuleIndex != *ref.Index {
			return sarifRule{}, -1, -1, errors.New("conflicting SARIF rule indices")
		}
		if id != "" && ref.ID != "" && id != ref.ID {
			return sarifRule{}, -1, -1, errors.New("conflicting SARIF rule ids")
		}
		if ref.ID != "" {
			id = ref.ID
		}
		index = ref.Index
	}
	if index == nil {
		index = entry.RuleIndex
	}
	ruleIndex := -1
	if index != nil {
		if *index < -1 || *index >= len(component.Rules) {
			return sarifRule{}, -1, -1, errors.New("SARIF rule index out of range")
		}
		ruleIndex = *index
	}
	if ruleIndex < 0 && ref != nil && ref.GUID != "" {
		for i := range component.Rules {
			if component.Rules[i].GUID == ref.GUID {
				ruleIndex = i
				break
			}
		}
		if ruleIndex < 0 {
			return sarifRule{}, -1, -1, errors.New("SARIF rule guid not found")
		}
	}
	if ruleIndex < 0 && (ref == nil || ref.GUID == "") {
		for i := range component.Rules {
			if id != "" && component.Rules[i].ID == id {
				ruleIndex = i
				break
			}
		}
	}
	if ruleIndex < 0 {
		return sarifRule{}, componentIndex, -1, nil
	}
	rule := component.Rules[ruleIndex]
	if ref != nil && ref.GUID != "" && rule.GUID != ref.GUID {
		return sarifRule{}, -1, -1, errors.New("conflicting SARIF rule references")
	}
	return rule, componentIndex, ruleIndex, nil
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
	if err != nil || parsed.Scheme != "file" || parsed.User != nil || parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") || parsed.RawQuery != "" || parsed.Fragment != "" {
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
