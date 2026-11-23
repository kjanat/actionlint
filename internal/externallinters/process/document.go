package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/languages/byteoffset"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

// DocumentRequest positions in dependencies use the captured document's YAML coordinates.
type DocumentRequest struct {
	Source       string             `json:"source"`
	Filename     string             `json:"filename"`
	Kind         string             `json:"kind"`
	Config       json.RawMessage    `json:"config,omitempty"`
	Directory    string             `json:"directory,omitempty"`
	Dependencies []facts.Dependency `json:"dependencies,omitempty"`
}

// DocumentAnalyzer never admits a finding or edit in an uncaptured file.
type DocumentAnalyzer struct {
	backend    *Backend
	descriptor checkinputs.Descriptor
	documents  []string
	directory  bool
}

func NewDocumentAnalyzer(manifest toolmanifest.Manifest, command *hostprocess.Command, options Options) (*DocumentAnalyzer, error) {
	if manifest.Role != toolmanifest.RoleDocument || manifest.Transport != toolmanifest.TransportProcess || manifest.Preparation != "generic" {
		return nil, errors.New("document providers require process transport and generic preparation")
	}
	backend, err := NewBackend(manifest, command, options)
	if err != nil {
		return nil, err
	}
	requires := []rule.InputKind{rule.Source}
	for _, fact := range manifest.Rule.RequiredInputs {
		if fact != rule.Source && fact != rule.Dependencies {
			return nil, fmt.Errorf("unsupported document input kind %q", fact)
		}
		if !slices.Contains(requires, fact) {
			requires = append(requires, fact)
		}
	}
	for _, kind := range manifest.Selectors.Documents {
		if kind != "workflow" && kind != "action" {
			return nil, fmt.Errorf("unsupported document selector %q", kind)
		}
	}
	scope, err := manifest.Scope.ConfigurationScope()
	if err != nil {
		return nil, fmt.Errorf("document provider %s: %w", manifest.Rule.Name, err)
	}
	descriptor := manifest.Rule.Clone()
	descriptor.RequiredInputs = requires
	return &DocumentAnalyzer{backend: backend, descriptor: checkinputs.Descriptor{InvocationDefinitions: commands.CloneDefinitions(manifest.InvocationDefinitions), Descriptor: descriptor, Scope: scope, SupportsActions: len(manifest.Selectors.Documents) == 0 || slices.Contains(manifest.Selectors.Documents, "action")}, documents: slices.Clone(manifest.Selectors.Documents), directory: slices.Contains(manifest.Capabilities.Resources, toolmanifest.ResourceDirectory) || slices.Contains(manifest.Capabilities.Resources, toolmanifest.ResourceWorkspace)}, nil
}

func (d *DocumentAnalyzer) Descriptor() checkinputs.Descriptor {
	return d.descriptor.Detached()
}

func (d *DocumentAnalyzer) AnalyzeDocument(ctx context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	if input.Kind != "workflow" && input.Kind != "action" {
		return nil, fmt.Errorf("unsupported document kind %q", input.Kind)
	}
	if len(d.documents) != 0 && !slices.Contains(d.documents, input.Kind) {
		return nil, nil
	}
	if !input.Coordinates.Valid() || !input.Revision.Valid() || !utf8.ValidString(input.Content) {
		return nil, errors.New("document provider requires captured source coordinates and revision")
	}
	if err := d.backend.ValidateConfig(ctx, input.Options); err != nil {
		return nil, err
	}
	request := DocumentRequest{Source: input.Content, Filename: input.Path, Kind: input.Kind, Config: slices.Clone(input.Options)}
	if d.directory {
		request.Directory = input.Directory
		if request.Directory == "" {
			request.Directory = filepath.Dir(input.Path)
		}
		if err := currentDocument(ctx, input); err != nil {
			return nil, err
		}
	}
	if slices.Contains(d.descriptor.RequiredInputs, rule.Dependencies) {
		request.Dependencies = make([]facts.Dependency, 0, len(input.Dependencies))
		for _, dependency := range input.Dependencies {
			if dependency.Reference.Document != input.Revision.Document() || dependency.Reference.Revision != input.Revision.Digest() {
				continue
			}
			request.Dependencies = append(request.Dependencies, dependency)
		}
	}
	transport := scriptengine.Request{Source: input.Content, Filename: input.Path, Directory: input.Directory, Config: slices.Clone(input.Options)}
	result, err := d.backend.run(ctx, transport, nil, &request)
	if d.directory {
		if freshness := currentDocument(ctx, input); freshness != nil {
			return nil, errors.Join(owned(ctx, err), freshness)
		}
	}
	findings := make([]results.Diagnostic, 0, len(result.Diagnostics))
	for _, entry := range result.Diagnostics {
		finding, mapErr := d.finding(input, entry)
		if mapErr != nil {
			err = errors.Join(err, mapErr)
			continue
		}
		findings = append(findings, finding)
	}
	return findings, owned(ctx, err)
}

// A directory-capable process can read the file and ignore stdin. Refuse stale
// disk content before running it and before binding its output to this capture.
func currentDocument(ctx context.Context, input checkinputs.AnalyzerInput) (err error) {
	path := input.Revision.Document()
	file, err := files.New(ctx, nil, nil).Open(path)
	if err != nil {
		return fmt.Errorf("document provider cannot verify captured source on disk: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	data, err := io.ReadAll(io.LimitReader(file, int64(len(input.Content))+1))
	if err != nil {
		return err
	}
	if string(data) != input.Content {
		return errors.New("document provider directory access requires on-disk source to match captured content")
	}
	return nil
}

func documentFindings(findings []scriptengine.Diagnostic, request scriptengine.Request, directory string) []scriptengine.Diagnostic {
	selected := make([]scriptengine.Diagnostic, 0, len(findings))
	for _, finding := range findings {
		name := finding.Filename
		if finding.Source != "" {
			name = finding.Source
		}
		if name != "" && !sameDocument(name, request.Filename, directory) {
			continue
		}
		finding.Filename, finding.Source = request.Filename, ""
		selected = append(selected, finding)
	}
	return selected
}

func sameDocument(name, filename, directory string) bool {
	if sameArtifact(name, filename) {
		return true
	}
	absolute := func(path string) string {
		if filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
		return filepath.Clean(filepath.Join(directory, path))
	}
	expected := absolute(filename)
	if sameArtifact(name, expected) {
		return true
	}
	if strings.Contains(name, "://") {
		return false
	}
	candidate := absolute(name)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(candidate, expected)
	}
	return candidate == expected
}

func (d *DocumentAnalyzer) finding(input checkinputs.AnalyzerInput, entry scriptengine.Diagnostic) (results.Diagnostic, error) {
	if len(entry.Fixes) != 0 && !d.backend.capabilities.Fixes {
		return results.Diagnostic{}, errors.New("document provider returned undeclared fixes")
	}
	if entry.TemplateEffect != "" && entry.TemplateEffect != scriptengine.TemplateEffectNone {
		return results.Diagnostic{}, errors.New("document provider returned a snippet template effect")
	}
	start, err := documentPosition(input, entry.Location)
	if err != nil {
		return results.Diagnostic{}, err
	}
	var end sourcetext.Position
	if entry.EndLocation != (sourcetext.Position{}) {
		end, err = documentPosition(input, entry.EndLocation)
		if err != nil {
			return results.Diagnostic{}, err
		}
		if _, _, err := byteoffset.Range([]byte(input.Content), input.Coordinates, start, end); err != nil {
			return results.Diagnostic{}, err
		}
	}
	level, _ := d.backend.capabilities.HostSeverity(entry.Severity)
	message := entry.Message
	if template := d.backend.capabilities.MessageTemplate; template != "" {
		message = strings.NewReplacer("{code}", entry.Code, "{severity}", entry.Severity, "{row}", strconv.Itoa(start.Line), "{column}", strconv.Itoa(start.Column), "{message}", entry.Message).Replace(template)
	}
	finding := results.Diagnostic{Rule: d.descriptor.Name, Code: entry.Code, Severity: level, Message: message, Path: input.Path, Start: start, End: end, DocumentationURL: entry.URL}
	for _, fix := range entry.Fixes {
		mapped := results.Fix{Applicability: fix.Applicability, Description: fix.Description, Revisions: map[string]string{input.Path: input.Revision.Digest()}}
		for _, replacement := range fix.Replacements {
			start, err := documentPosition(input, replacement.Start)
			if err != nil {
				return results.Diagnostic{}, err
			}
			end, err := documentPosition(input, replacement.End)
			if err != nil {
				return results.Diagnostic{}, err
			}
			if _, _, err := byteoffset.Range([]byte(input.Content), input.Coordinates, start, end); err != nil {
				return results.Diagnostic{}, err
			}
			mapped.Edits = append(mapped.Edits, results.Edit{Path: input.Path, Start: start, End: end, Replacement: replacement.Text})
		}
		if len(mapped.Edits) == 0 {
			return results.Diagnostic{}, errors.New("document provider returned an empty fix")
		}
		finding.Fixes = append(finding.Fixes, mapped)
	}
	return finding, nil
}

// Process decoders normalize positions to LF/scalar coordinates. Resolve their
// exact byte offset, then map it to the captured document's coordinate model.
func documentPosition(input checkinputs.AnalyzerInput, point sourcetext.Position) (sourcetext.Position, error) {
	offset, err := byteoffset.Offset([]byte(input.Content), sourcetext.RawLF, point)
	if err != nil {
		return sourcetext.Position{}, err
	}
	if input.Coordinates == sourcetext.RawLF {
		return point, nil
	}
	document := sourcemap.NewDocument(input.Content)
	begin := 0
	if strings.HasPrefix(input.Content, "\ufeff") {
		begin = 3
	}
	for row := range document.LineCount() {
		text, ending := document.Line(row)
		if offset >= begin && offset <= begin+len(text) {
			return sourcetext.Position{Line: row + 1, Column: utf8.RuneCountInString(input.Content[begin:offset]) + 1}, nil
		}
		begin += len(text) + len(ending)
	}
	return sourcetext.Position{}, fmt.Errorf("document position %v lies inside a physical line break or BOM", point)
}
