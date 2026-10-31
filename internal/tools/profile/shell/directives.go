package shell

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	directiveCodeSelector = regexp.MustCompile(`^(all|(SC)?[0-9]+(-(SC)?[0-9]+)?)$`)
	directiveOptionalName = regexp.MustCompile(`^[a-zA-Z-]+$`)
)

// Directives is a typed representation of project-wide ShellCheck directives.
type Directives struct {
	// Disable lists codes, code ranges or all, for example [SC2086, SC3000-SC4000].
	Disable []string `json:"disable,omitempty" yaml:"disable" jsonschema:"nullable,pattern=^(all|(SC)?[0-9]+(-(SC)?[0-9]+)?)$"`
	// Enable selects optional check names. Scalar all or true expands to [all].
	// This selection does not change the tool enabled setting.
	Enable []string `json:"enable,omitempty" yaml:"enable" jsonschema:"nullable,pattern=^[a-zA-Z-]+$"`
	// Shell overrides the inferred dialect of scripts selected for ShellCheck.
	Shell *string `json:"shell,omitempty" yaml:"shell" jsonschema:"nullable,enum=sh,enum=bash,enum=dash,enum=ksh,enum=busybox"`
	// ExtendedAnalysis enables or disables ShellCheck dataflow analysis. Omission keeps its default.
	ExtendedAnalysis *bool `json:"extended-analysis,omitempty" yaml:"extended-analysis" jsonschema:"nullable"`
	// ExternalSources controls following sourced files. Omission preserves actionlint's enabled default.
	ExternalSources *bool `json:"external-sources,omitempty" yaml:"external-sources" jsonschema:"nullable"`
	// SourcePath lists directories searched for sourced files, relative to the run step's effective working directory.
	// Scripts arrive on stdin, so SCRIPTDIR does not denote the workflow file's directory.
	SourcePath []string `json:"source-path,omitempty" yaml:"source-path" jsonschema:"nullable,minLength=1"`
}

// ParseDirectives decodes and validates inline directive settings. Errors
// name the offending key first, so callers prefix them with the settings'
// own path.
func ParseDirectives(data json.RawMessage) (*Directives, error) {
	var raw struct {
		Disable          []string        `json:"disable"`
		Enable           json.RawMessage `json:"enable"`
		Shell            *string         `json:"shell"`
		ExtendedAnalysis *bool           `json:"extended-analysis"`
		ExternalSources  *bool           `json:"external-sources"`
		SourcePath       []string        `json:"source-path"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	directives := &Directives{Disable: raw.Disable, Shell: raw.Shell, ExtendedAnalysis: raw.ExtendedAnalysis, ExternalSources: raw.ExternalSources, SourcePath: raw.SourcePath}
	switch enable := string(bytes.TrimSpace(raw.Enable)); enable {
	case "", "null":
	case `"all"`, "true":
		directives.Enable = []string{"all"}
	default:
		if err := json.Unmarshal(raw.Enable, &directives.Enable); err != nil {
			return nil, fmt.Errorf("enable: %w", err)
		}
	}
	if err := validateDirectives(directives); err != nil {
		return nil, err
	}
	return directives, nil
}

// validateDirectives reports the first setting ShellCheck cannot accept as
// a directive.
func validateDirectives(d *Directives) error {
	for _, code := range d.Disable {
		if !directiveCodeSelector.MatchString(code) {
			return fmt.Errorf("disable: invalid code selector %q", code)
		}
	}
	for _, name := range d.Enable {
		if !directiveOptionalName.MatchString(name) {
			return fmt.Errorf("enable: invalid check name %q", name)
		}
	}
	if d.Shell != nil && !slices.Contains([]string{"sh", "bash", "dash", "ksh", "busybox"}, *d.Shell) {
		return fmt.Errorf("shell: unknown dialect %q", *d.Shell)
	}
	for _, path := range d.SourcePath {
		if _, err := directivePath(path); err != nil {
			return err
		}
	}
	return nil
}

func directivePath(path string) (string, error) {
	if path == "" || strings.ContainsAny(path, "\r\n\x00") {
		return "", errors.New("source-path: expected a nonempty, single-line path")
	}
	// ShellCheck's directive parser does not interpret escapes inside quotes.
	if !strings.Contains(path, `"`) {
		return `"` + path + `"`, nil
	}
	if !strings.Contains(path, "'") {
		return "'" + path + "'", nil
	}
	if !strings.ContainsAny(path, " \t") && path[0] != '\'' && path[0] != '"' {
		return path, nil
	}
	return "", fmt.Errorf("source-path: ShellCheck cannot represent path %q containing whitespace and both quote types", path)
}

// RenderDirectives renders settings as ShellCheck directive comments; nil
// renders none.
func RenderDirectives(d *Directives) (string, error) {
	if d == nil {
		return "", nil
	}
	var out strings.Builder
	write := func(key, value string) {
		out.WriteString("# shellcheck ")
		out.WriteString(key)
		out.WriteByte('=')
		out.WriteString(value)
		out.WriteByte('\n')
	}
	if len(d.Disable) > 0 {
		write("disable", strings.Join(d.Disable, ","))
	}
	if len(d.Enable) > 0 {
		write("enable", strings.Join(d.Enable, ","))
	}
	if d.ExtendedAnalysis != nil {
		write("extended-analysis", strconv.FormatBool(*d.ExtendedAnalysis))
	}
	for _, path := range d.SourcePath {
		encoded, err := directivePath(path)
		if err != nil {
			return "", err
		}
		write("source-path", encoded)
	}
	return out.String(), nil
}
